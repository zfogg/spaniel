package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFileManagedDashboardMutationsAreRejected(t *testing.T) {
	handler, _ := setupRouter(t)
	for name, test := range map[string]struct {
		method, path, body string
	}{
		"dashboard": {http.MethodPatch, "/api/dashboards/file-owned", `{"name":"no"}`},
		"delete":    {http.MethodDelete, "/api/dashboards/file-owned", ""},
		"panel":     {http.MethodPost, "/api/dashboards/file-owned/panels", `{"title":"no","display_type":"table","query_sql":"SELECT 1"}`},
		"variable":  {http.MethodPost, "/api/dashboards/file-owned/variables", `{"name":"x","kind":"string","source":"x"}`},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusConflict {
				t.Fatalf("status %d want %d: %s", w.Code, http.StatusConflict, w.Body.String())
			}
		})
	}
}

func TestDeployCorrelationPanelSaveValidatesBothQueries(t *testing.T) {
	handler, db := setupRouter(t)
	dashboard, err := db.CreateDashboardWithPanels("Deploys", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/dashboards/"+dashboard.ID+"/panels", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	valid := `{"title":"Deploys","display_type":"deploy_correlation","query_sql":"SELECT 1 AS timestamp_ns, 1 AS value","settings_json":"{\"annotation_query\":\"SELECT 1 AS timestamp_ns, 'v1.2.3' AS release\",\"annotation_label\":\"Releases\"}"}`
	if w := run(valid); w.Code != http.StatusOK {
		t.Fatalf("valid deploy panel rejected: %d %s", w.Code, w.Body.String())
	}
	for name, body := range map[string]string{
		"main-shape":       `{"title":"Deploys","display_type":"deploy_correlation","query_sql":"SELECT 1 AS value","settings_json":"{}"}`,
		"annotation-shape": `{"title":"Deploys","display_type":"deploy_correlation","query_sql":"SELECT 1 AS timestamp_ns, 1 AS value","settings_json":"{\"annotation_query\":\"SELECT 1 AS timestamp_ns\"}"}`,
		"annotation-write": `{"title":"Deploys","display_type":"deploy_correlation","query_sql":"SELECT 1 AS timestamp_ns, 1 AS value","settings_json":"{\"annotation_query\":\"DELETE FROM spans\"}"}`,
		"settings-array":   `{"title":"Deploys","display_type":"deploy_correlation","query_sql":"SELECT 1 AS timestamp_ns, 1 AS value","settings_json":"[]"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w := run(body); w.Code != http.StatusBadRequest {
				t.Fatalf("status %d want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
		})
	}
}

func TestDeployCorrelationImportValidatesAnnotationQuery(t *testing.T) {
	handler, _ := setupRouter(t)
	for name, yaml := range map[string]string{
		"valid":   "version: 1\nname: Deploys\npanels:\n  - title: Deploys\n    display_type: deploy_correlation\n    query: SELECT 1 AS timestamp_ns, 1 AS value\n    settings:\n      annotation_query: SELECT 1 AS timestamp_ns, 'v1' AS version\n",
		"invalid": "version: 1\nname: Deploys\npanels:\n  - title: Deploys\n    display_type: deploy_correlation\n    query: SELECT 1 AS timestamp_ns, 1 AS value\n    settings:\n      annotation_query: SELECT 1 AS timestamp_ns\n",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/dashboards/import", bytes.NewBufferString(yaml))
			req.Header.Set("Content-Type", "application/yaml")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			want := http.StatusBadRequest
			if name == "valid" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
			}
		})
	}
}
