package api

import (
	"encoding/json"
	"errors"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"net/http"
	"testing"
)

func TestDefinitionDetailAcknowledgesUpdate(t *testing.T) {
	handler, db := setupRouter(t)
	dashboard := &storage.Dashboard{ID: "file-dashboard", Name: "Dashboard", SourceFile: "dashboard.yaml", SourceHash: "hash"}
	if err := db.ReplaceDashboardDefinition(dashboard); err != nil {
		t.Fatal(err)
	}
	rule := &storage.AlertRule{ID: "file-alert", Name: "Alert", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: "[]", SourceFile: "alert.yaml", SourceHash: "hash"}
	if err := db.ReplaceAlertDefinition(rule); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/dashboards/file-dashboard", "/api/alerts/file-alert"} {
		for i, want := range []bool{true, false} {
			response := do(t, handler, http.MethodGet, path, nil)
			if response.Code != 200 {
				t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
			}
			var out struct {
				Data struct {
					Updated bool `json:"updated"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Data.Updated != want {
				t.Fatalf("%s visit %d: updated=%v, want %v", path, i, out.Data.Updated, want)
			}
		}
	}
}

func TestDefinitionReloadEndpoints(t *testing.T) {
	_, db := setupRouter(t)
	for _, kind := range []string{"dashboard", "alert"} {
		for _, fail := range []bool{false, true} {
			calls := 0
			reload := func() error {
				calls++
				if fail {
					return errors.New("invalid YAML")
				}
				return nil
			}
			settings := &SettingsService{ReloadDashboards: reload, ReloadAlerts: reload}
			handler := NewRouterFull(db, ws.NewHub(), nil, nil, settings, nil, nil, nil)
			response := do(t, handler, http.MethodPost, "/api/"+kind+"s/reload", map[string]any{})
			want := 200
			if fail {
				want = 400
			}
			if response.Code != want || calls != 1 {
				t.Fatalf("%s reload: status=%d calls=%d", kind, response.Code, calls)
			}
		}
	}
}
