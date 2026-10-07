package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReorderDashboardList(t *testing.T) {
	handler, db := setupRouter(t)
	a, err := db.CreateDashboard("A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateDashboard("B", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"ids":["` + a.ID + `","` + b.ID + `"]}`, 200},
		{`{"ids":["` + a.ID + `","` + a.ID + `"]}`, 400},
		{`{"ids":["missing"]}`, 400},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/dashboards/reorder", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
	}
	got, err := db.ListDashboards()
	if err != nil || got[0].ID != a.ID {
		t.Fatalf("invalid requests changed order: %v", err)
	}
}
