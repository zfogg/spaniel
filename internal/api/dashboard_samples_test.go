package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestSingleValuePanelHistoryPreview(t *testing.T) {
	h, db := setupRouter(t)
	session, err := db.CreateSession("history", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	dashboard, err := db.CreateDashboardWithPanels("history", "", []*storage.DashboardPanel{
		{Title: "value", DisplayType: "single_value", QuerySQL: "SELECT 1 AS value", SettingsJSON: `{"show_history":true}`, LayoutJSON: `{}`},
		{Title: "disabled", DisplayType: "single_value", QuerySQL: "SELECT 1 AS value", SettingsJSON: `{}`, LayoutJSON: `{}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	type previewResponse struct {
		Data struct {
			History  []storage.DashboardSamplePoint `json:"history"`
			Warnings []string                       `json:"warnings"`
		} `json:"data"`
	}
	preview := func(panelID, query string) previewResponse {
		body := fmt.Sprintf(`{"panel_id":%q,"display_type":"single_value","query_sql":%q}`, panelID, query)
		req := httptest.NewRequest("POST", "/api/dashboards/"+dashboard.ID+"/query-preview", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != 200 {
			t.Fatalf("preview: %d %s", res.Code, res.Body.String())
		}
		var out previewResponse
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := preview(dashboard.Panels[0].ID, "SELECT 1 AS value")
	if len(first.Data.History) != 1 || first.Data.History[0].Value != 1 || len(first.Data.Warnings) != 0 {
		t.Fatalf("first history: %#v", first)
	}
	repeated := preview(dashboard.Panels[0].ID, "SELECT 1 AS value")
	if len(repeated.Data.History) != 1 {
		t.Fatalf("duplicate in same minute: %#v", repeated)
	}
	different := preview(dashboard.Panels[0].ID, "SELECT 2 AS value")
	if len(different.Data.History) != 1 || different.Data.History[0].Value != 2 {
		t.Fatalf("query isolation: %#v", different)
	}
	disabled := preview(dashboard.Panels[1].ID, "SELECT 1 AS value")
	if len(disabled.Data.History) != 0 {
		t.Fatalf("disabled panel produced history: %#v", disabled)
	}
	secondSession, err := db.CreateSession("other", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(secondSession.ID, secondSession.Label)
	isolated := preview(dashboard.Panels[0].ID, "SELECT 1 AS value")
	if len(isolated.Data.History) != 1 || isolated.Data.History[0].Value != 1 {
		t.Fatalf("session isolation: %#v", isolated)
	}
	var sampleCount int
	if err := db.SQL().QueryRow("SELECT COUNT(*) FROM dashboard_panel_samples").Scan(&sampleCount); err != nil || sampleCount != 3 {
		t.Fatalf("sample series count=%d err=%v", sampleCount, err)
	}
	missing := preview("other", "SELECT 1 AS value")
	if len(missing.Data.History) != 0 {
		t.Fatalf("unknown panel produced samples: %#v", missing)
	}
}
