package api

import (
	"bytes"
	"encoding/json"
	"github.com/zfogg/spaniel/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateDashboardTemplate(t *testing.T) {
	handler, db := setupRouter(t)
	send := func(body any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/dashboards", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	p := map[string]any{"title": "Count", "display_type": "single_value", "query_sql": "SELECT count(*) AS value FROM spans"}
	panels := []any{p, p, p, p, p, p, p, p}
	w := send(map[string]any{"name": "Office", "panels": panels})
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data storage.Dashboard `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetDashboard(result.Data.ID)
	if err != nil || len(loaded.Panels) != 8 {
		t.Fatalf("persisted: %+v %v", loaded, err)
	}
	ids := map[string]bool{}
	if len(loaded.Variables) != 4 {
		t.Fatalf("expected four defaults, got %+v", loaded.Variables)
	}
	defaults := map[string]string{"service": "", "operation": "", "status_code": "0", "severity": "9"}
	for _, variable := range loaded.Variables {
		value, ok := defaults[variable.Name]
		if !ok || variable.DefaultValue != value {
			t.Fatalf("unexpected default: %+v", variable)
		}
	}
	for i, panel := range loaded.Panels {
		if ids[panel.ID] || panel.Position != i || panel.QueryVersion != 1 {
			t.Fatalf("invalid panel: %+v", panel)
		}
		ids[panel.ID] = true
	}
	before, _ := db.ListDashboards()
	for _, invalid := range []any{
		[]any{p, map[string]any{"title": "Bad", "display_type": "single_value", "query_sql": "DELETE FROM spans"}},
		[]any{map[string]any{"title": "Bad", "display_type": "unsupported", "query_sql": "SELECT 1"}},
		[]any{p, p, p, p, p, p, p, p, p, p, p},
	} {
		w = send(map[string]any{"name": "Invalid", "panels": invalid})
		if w.Code != 400 {
			t.Fatalf("expected validation failure: %d %s", w.Code, w.Body.String())
		}
	}
	after, _ := db.ListDashboards()
	if len(after) != len(before) {
		t.Fatal("invalid template left a dashboard behind")
	}
	w = send(map[string]any{"name": "None", "panels": []any{}})
	if w.Code != 200 {
		t.Fatalf("empty: %s", w.Body.String())
	}
}
