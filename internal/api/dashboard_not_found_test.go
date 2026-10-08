package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestDashboardChildEndpointsRejectMissingResources(t *testing.T) {
	handler, db := setupRouter(t)
	dashboard, err := db.CreateDashboard("Child resources", "")
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		method, path, body string
	}{
		"create variable for missing dashboard": {http.MethodPost, "/api/dashboards/missing/variables", `{"name":"region","kind":"string","source":"spans.service_name"}`},
		"delete missing variable":               {http.MethodDelete, "/api/dashboards/" + dashboard.ID + "/variables/missing", ""},
		"delete variable for missing dashboard": {http.MethodDelete, "/api/dashboards/missing/variables/service", ""},
		"delete missing panel":                  {http.MethodDelete, "/api/dashboards/" + dashboard.ID + "/panels/missing", ""},
		"update missing panel":                  {http.MethodPatch, "/api/dashboards/" + dashboard.ID + "/panels/missing", `{"title":"Missing","display_type":"table","query_sql":"SELECT 1"}`},
		"delete missing dashboard":              {http.MethodDelete, "/api/dashboards/missing", ""},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status %d want %d: %s", w.Code, http.StatusNotFound, w.Body.String())
			}
		})
	}

	panel := &storage.DashboardPanel{DashboardID: dashboard.ID, Title: "Existing", DisplayType: "table", QuerySQL: "SELECT 1"}
	if err := db.CreateDashboardPanel(panel); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/dashboards/"+dashboard.ID+"/panels/"+panel.ID, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete existing panel status %d: %s", w.Code, w.Body.String())
	}
}
