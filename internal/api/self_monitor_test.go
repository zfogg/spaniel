package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSelfMonitorDefinitions(t *testing.T) {
	handler, _, store := newSettingsRouter(t)
	// Match by name even when the existing ID differs from the bundled ID.
	dashboard, err := store.CreateDashboardWithPanels("Spaniel runtime and storage", "user edits", nil)
	if err != nil {
		t.Fatal(err)
	}
	install := func(kind string, created, refreshed, skipped int) {
		t.Helper()
		response := do(t, handler, http.MethodPost, "/api/settings/self-monitor/"+kind, nil)
		if response.Code != 200 {
			t.Fatalf("%s: %s", kind, response.Body.String())
		}
		var envelope struct {
			Data selfMonitorInstallResult `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data != (selfMonitorInstallResult{Created: created, Refreshed: refreshed, Skipped: skipped}) {
			t.Fatalf("%s: %+v", kind, envelope.Data)
		}
	}
	install("dashboards", 2, 1, 0)
	got, err := store.GetDashboard(dashboard.ID)
	if err != nil || len(got.Panels) == 0 || got.Description == "user edits" {
		t.Fatalf("dashboard was not refreshed: %+v, %v", got, err)
	}
	got.Panels = nil
	got.Description = "edited again"
	if err := store.ReplaceDashboardDefinition(got); err != nil {
		t.Fatal(err)
	}
	install("dashboards", 0, 3, 0)
	got, err = store.GetDashboard(dashboard.ID)
	if err != nil || len(got.Panels) == 0 || got.Description == "edited again" {
		t.Fatal("refresh did not restore bundled panels")
	}
	install("alerts", 3, 0, 0)
	rules, err := store.ListAlertRules()
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		if _, _, _, err := store.ReadOnlyQuery(context.Background(), rule.QuerySQL, 10); err != nil {
			t.Fatalf("%s SQL: %v", rule.Name, err)
		}
	}
	rule := rules[0]
	originalSQL := rule.QuerySQL
	originalCreatedAt := rule.CreatedAt
	rule.Enabled = false
	rule.QuerySQL = "SELECT 42 AS value"
	if err := store.ReplaceAlertDefinition(rule); err != nil {
		t.Fatal(err)
	}
	install("alerts", 0, 3, 0)
	refreshed, err := store.GetAlertRule(rule.ID)
	if err != nil || !refreshed.Enabled || refreshed.QuerySQL != originalSQL || refreshed.CreatedAt != originalCreatedAt {
		t.Fatalf("existing alert was not refreshed in place: %+v, %v", refreshed, err)
	}
	rules, err = store.ListAlertRules()
	if err != nil || len(rules) != 3 {
		t.Fatalf("duplicate alerts: %v", err)
	}
	dashboards, err := store.ListDashboards()
	if err != nil || len(dashboards) != 3 {
		t.Fatalf("duplicate dashboards: %v", err)
	}
}
