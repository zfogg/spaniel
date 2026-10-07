package dashboardconfig

import (
	"github.com/zfogg/spaniel/internal/model"
	"strings"
	"testing"
)

func TestDashboardYAMLRoundTripExcludesData(t *testing.T) {
	d := &model.Dashboard{ID: "db-id", Name: "Service health", Description: "The important things", Variables: []*model.DashboardVariable{{Name: "service", Kind: "enum", Source: "spans.service_name", OptionsJSON: "[\"api\"]", DefaultValue: "api"}}, Panels: []*model.DashboardPanel{{ID: "panel-id", Title: "Errors", DisplayType: "single_value", QuerySQL: "SELECT count(*) AS value FROM spans", SettingsJSON: "{\"unit\":\"requests\"}", LayoutJSON: "{\"w\":6}", Position: 2}}}
	data, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, unwanted := range []string{"db-id", "panel-id", "created_at", "updated_at"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("export leaks %q: %s", unwanted, text)
		}
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := parsed.Dashboard("new-id")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != d.Name || len(restored.Variables) != 1 || len(restored.Panels) != 1 || restored.Panels[0].SettingsJSON == "{}" {
		t.Fatalf("definition did not round trip: %#v", restored)
	}
}
