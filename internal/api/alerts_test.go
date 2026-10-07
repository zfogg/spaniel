package api

import (
	"net/http"
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
