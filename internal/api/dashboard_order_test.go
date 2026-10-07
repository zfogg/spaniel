package api

import (
	"github.com/zfogg/spaniel/internal/storage"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMoveDashboardPanelEndpoint(t *testing.T) {
	handler, db := setupRouter(t)
	d, err := db.CreateDashboardWithPanels("Order", "", []*storage.DashboardPanel{
		{Title: "A", QuerySQL: "SELECT 1", DisplayType: "single_value"},
		{Title: "B", QuerySQL: "SELECT 2", DisplayType: "single_value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		dashboard, panel, body string
		status                 int
	}{
		{d.ID, d.Panels[1].ID, `{"direction":-1}`, 200},
		{d.ID, d.Panels[1].ID, `{"direction":0}`, 400},
		{d.ID, "missing", `{"direction":1}`, 404},
		{"missing", d.Panels[0].ID, `{"direction":1}`, 404},
	} {
		req := httptest.NewRequest("POST", "/api/dashboards/"+tc.dashboard+"/panels/"+tc.panel+"/move", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	got, err := db.GetDashboard(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Panels[0].ID != d.Panels[1].ID || got.Panels[1].ID != d.Panels[0].ID {
		t.Fatal("order was not persisted")
	}
}
