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

func TestParseDashboardYAMLValidation(t *testing.T) {
	valid := `version: 1
name: Service health
variables:
  - name: service
    kind: enum
    source: spans.service_name
    options: [api]
    default_value: api
panels:
  - title: Errors
    display_type: single_value
    query: SELECT count(*) AS value FROM spans
    settings: {}
    layout: {}
    position: 0
  - title: Latency
    display_type: time_series
    query: SELECT timestamp_ns, count(*) AS value FROM spans GROUP BY 1
    position: 1
`
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
	for name, yaml := range map[string]string{
		"unknown-field":        strings.Replace(valid, "name: Service health", "name: Service health\nowner: nobody", 1),
		"duplicate-position":   strings.Replace(valid, "position: 1", "position: 0", 1),
		"position-gap":         strings.Replace(valid, "position: 1", "position: 2", 1),
		"bad-variable":         strings.Replace(valid, "name: service", "name: service-name", 1),
		"magic-collision":      strings.Replace(valid, "name: service", "name: service_name", 1),
		"bad-display":          strings.Replace(valid, "display_type: single_value", "display_type: chart", 1),
		"scalar-settings":      strings.Replace(valid, "settings: {}", "settings: compact", 1),
		"invalid-enum-default": strings.Replace(valid, "default_value: api", "default_value: web", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(yaml)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
