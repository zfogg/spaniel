package mcp

import (
	"github.com/zfogg/spaniel/internal/storage"
	"testing"
)

func TestMCPDefinitionUpdateFirstAccess(t *testing.T) {
	cs, db := connect(t, Options{Version: "test"})
	if err := db.ReplaceDashboardDefinition(&storage.Dashboard{ID: "watched", Name: "Watched", SourceFile: "dashboard.yaml", SourceHash: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertDefinition(&storage.AlertRule{ID: "watched", Name: "Watched", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: "[]", SourceFile: "alert.yaml", SourceHash: "one"}); err != nil {
		t.Fatal(err)
	}
	list := callStructured[ListDashboardsOutput](t, cs, "list_dashboards", nil)
	if len(list.Dashboards) != 1 || !list.Dashboards[0].Updated {
		t.Fatal("list omitted unread revision")
	}
	for _, want := range []bool{true, false} {
		dashboard := callStructured[DashboardOutput](t, cs, "get_dashboard", map[string]any{"dashboard_id": "watched"})
		if dashboard.Dashboard.Updated != want {
			t.Fatalf("dashboard updated=%v, want %v", dashboard.Dashboard.Updated, want)
		}
		alert := callStructured[AlertOutput](t, cs, "get_alert", map[string]any{"alert_id": "watched"})
		if alert.Rule.Updated != want {
			t.Fatalf("alert updated=%v, want %v", alert.Rule.Updated, want)
		}
	}
	dashboard, err := db.GetDashboard("watched")
	if err != nil || dashboard.Updated {
		t.Fatal("MCP access did not acknowledge for other clients")
	}
}
