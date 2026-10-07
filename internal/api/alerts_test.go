package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func alertPayload() map[string]any {
	return map[string]any{
		"name":               "API error rate",
		"query_sql":          "SELECT service_name, count(*) AS value FROM telemetry_spans GROUP BY service_name",
		"condition":          map[string]any{"kind": "threshold", "operator": ">=", "value": 3},
		"group_by":           []string{"service_name"},
		"pending_for_ns":     int64(30_000_000_000),
		"cooldown_ns":        int64(300_000_000_000),
		"repeat_interval_ns": int64(900_000_000_000),
		"severity":           "critical",
		"enabled":            true,
		"browser_enabled":    true,
		"pushover_enabled":   false,
		"annotations":        map[string]string{"runbook": "https://example.test/runbook"},
	}
}

func TestAlertsAPI_CreateExportAndPreview(t *testing.T) {
	handler, _ := setupRouter(t)
	payload := alertPayload()

	created := do(t, handler, http.MethodPost, "/api/alerts", payload)
	if created.Code != http.StatusOK {
		t.Fatalf("create alert: status=%d body=%s", created.Code, created.Body.String())
	}
	rule := decodeData[map[string]any](t, created.Body.Bytes())
	id, _ := rule["id"].(string)
	if id == "" {
		t.Fatalf("created alert missing id: %#v", rule)
	}
	if updatedAt, ok := rule["updated_at"].(float64); !ok || updatedAt < 1_000_000_000_000_000_000 {
		t.Fatalf("updated_at must be nanoseconds, got %#v", rule["updated_at"])
	}
	if _, exists := rule["owner"]; exists {
		t.Fatalf("alert response unexpectedly exposes owner: %#v", rule)
	}
	if _, exists := rule["team"]; exists {
		t.Fatalf("alert response unexpectedly exposes team: %#v", rule)
	}

	exported := do(t, handler, http.MethodGet, "/api/alerts/"+id+"/config", nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export alert YAML: status=%d body=%s", exported.Code, exported.Body.String())
	}
	for _, want := range []string{"id: " + id, "threshold: 3", "pending_for: 30s", "cooldown: 5m0s", "repeat_interval: 15m0s", "runbook:"} {
		if !strings.Contains(exported.Body.String(), want) {
			t.Errorf("export missing %q:\n%s", want, exported.Body.String())
		}
	}
	if strings.Contains(exported.Body.String(), "owner:") || strings.Contains(exported.Body.String(), "team:") {
		t.Fatalf("export contains unsupported ownership metadata:\n%s", exported.Body.String())
	}

	preview := do(t, handler, http.MethodPost, "/api/alerts/preview", payload)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview draft: status=%d body=%s", preview.Code, preview.Body.String())
	}
	result := decodeData[map[string]any](t, preview.Body.Bytes())
	if columns, ok := result["columns"].([]any); !ok || len(columns) != 2 {
		t.Fatalf("preview columns = %#v, want service_name and value", result["columns"])
	}
}

func TestAlertsAPI_RejectsInvalidInstanceDiscovery(t *testing.T) {
	handler, _ := setupRouter(t)
	payload := alertPayload()
	payload["group_by"] = []string{}
	payload["instance_discovery"] = map[string]any{
		"query":          "SELECT DISTINCT service_name FROM telemetry_spans",
		"every_ns":       int64(15_000_000_000),
		"stale_after_ns": int64(3_600_000_000_000),
	}

	w := do(t, handler, http.MethodPost, "/api/alerts", payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "instance discovery requires") {
		t.Fatalf("unexpected validation error: %s", w.Body.String())
	}
}

func TestAlertsAPI_RejectsDefinitionsThatWouldFailAtEvaluation(t *testing.T) {
	handler, _ := setupRouter(t)
	for name, mutate := range map[string]func(map[string]any){
		"grouped no-data": func(payload map[string]any) {
			payload["condition"] = map[string]any{"kind": "no_data"}
		},
		"grouped composite": func(payload map[string]any) {
			payload["condition"] = map[string]any{"kind": "all_of", "rule_ids": []string{"a", "b"}}
		},
		"duplicate groups": func(payload map[string]any) {
			payload["group_by"] = []string{"service_name", "SERVICE_NAME"}
		},
		"negative lifecycle duration": func(payload map[string]any) {
			payload["cooldown_ns"] = int64(-1)
		},
		"invalid log-match operator": func(payload map[string]any) {
			payload["condition"] = map[string]any{"kind": "log_match", "pattern": "timeout", "operator": "~"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload := alertPayload()
			mutate(payload)
			w := do(t, handler, http.MethodPost, "/api/alerts", payload)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestNotificationPreviewHonorsGlobalDelivery(t *testing.T) {
	previous := currentAlertDelivery()
	t.Cleanup(func() { ConfigureAlertDelivery(previous) })
	rule := &storage.AlertRule{BrowserEnabled: true, PushoverEnabled: true}
	ConfigureAlertDelivery(AlertDelivery{
		BrowserEnabled:   func() bool { return false },
		PushoverEnabled:  func() bool { return false },
		PushoverUserKey:  "user",
		PushoverAPIToken: "token",
	})
	preview := notificationPreview(rule)
	if preview[0]["status"] != "suppressed" || preview[1]["status"] != "suppressed" {
		t.Fatalf("disabled global delivery preview=%#v", preview)
	}
	ConfigureAlertDelivery(AlertDelivery{
		BrowserEnabled:   func() bool { return true },
		PushoverEnabled:  func() bool { return true },
		PushoverUserKey:  "user",
		PushoverAPIToken: "token",
	})
	preview = notificationPreview(rule)
	if preview[0]["status"] != "would_send" || preview[1]["status"] != "would_send" {
		t.Fatalf("enabled global delivery preview=%#v", preview)
	}
}

func TestAlertQueryArgs_BindsActiveSession(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "parameters.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	session, err := store.CreateSession("active", false)
	if err != nil {
		t.Fatal(err)
	}
	store.SetActiveSession(session.ID, session.Label)
	columns, rows, _, err := store.ReadOnlyQueryArgs(context.Background(), "SELECT $session_id AS value", alertQueryArgs(store, "SELECT $session_id AS value"), 10)
	if err != nil || len(columns) != 1 || len(rows) != 1 || rows[0][0] != session.ID {
		t.Fatalf("alert session parameter columns=%v rows=%v err=%v", columns, rows, err)
	}
}

func TestAlertsAPI_AcknowledgementIsInstanceMetadata(t *testing.T) {
	handler, store := setupRouter(t)
	payload := alertPayload()
	created := do(t, handler, http.MethodPost, "/api/alerts", payload)
	if created.Code != http.StatusOK {
		t.Fatalf("create alert: %s", created.Body.String())
	}
	rule := decodeData[map[string]any](t, created.Body.Bytes())
	id := rule["id"].(string)
	value := 5.0
	if err := store.UpsertAlertInstance(&storage.AlertInstance{RuleID: id, GroupKey: "service_name=api", LabelsJSON: `{"service_name":"api"}`, State: "firing", Value: &value}); err != nil {
		t.Fatalf("seed alert instance: %v", err)
	}

	ack := do(t, handler, http.MethodPost, "/api/alerts/"+id+"/instances/acknowledge", map[string]any{"group_key": "service_name=api", "note": "on it"})
	if ack.Code != http.StatusOK {
		t.Fatalf("acknowledge instance: status=%d body=%s", ack.Code, ack.Body.String())
	}
	got, err := store.GetAlertRule(id)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if len(got.Instances) != 1 || got.Instances[0].State != "firing" || got.Instances[0].AcknowledgedAt == nil {
		t.Fatalf("acknowledgement must not alter lifecycle state: %#v", got.Instances)
	}
}
