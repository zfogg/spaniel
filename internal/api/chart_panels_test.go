package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestChartPanelsCreatePersistAndPreview(t *testing.T) {
	handler, db := setupRouter(t)
	send := func(path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, kind := range []string{"bar", "pie", "gauge"} {
		t.Run(kind, func(t *testing.T) {
			query := "SELECT 'orders' AS label, 42 AS value"
			response := send("/api/dashboards", map[string]any{"name": kind, "panels": []any{map[string]any{"title": kind, "display_type": kind, "query_sql": query, "settings_json": "{\"min\":0,\"max\":200}"}}})
			if response.Code != 200 {
				t.Fatalf("create: %d %s", response.Code, response.Body.String())
			}
			var result struct {
				Data storage.Dashboard `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			stored, err := db.GetDashboard(result.Data.ID)
			if err != nil || len(stored.Panels) != 1 {
				t.Fatalf("persist: %+v %v", stored, err)
			}
			if stored.Panels[0].DisplayType != kind || stored.Panels[0].SettingsJSON != "{\"min\":0,\"max\":200}" {
				t.Fatalf("lost panel settings: %+v", stored.Panels[0])
			}
			response = send("/api/dashboards/"+stored.ID+"/query-preview", map[string]any{"display_type": kind, "query_sql": query})
			if response.Code != 200 {
				t.Fatalf("preview: %d %s", response.Code, response.Body.String())
			}
			response = send("/api/dashboards/"+stored.ID+"/query-preview", map[string]any{"display_type": kind, "query_sql": "SELECT 42 AS missing"})
			if response.Code != 400 {
				t.Fatalf("invalid result: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
