package dashboardconfig

import (
	"fmt"
	"testing"
)

func TestChartPanelDefinitionsAndResults(t *testing.T) {
	for _, kind := range []string{"bar", "pie", "gauge"} {
		t.Run(kind, func(t *testing.T) {
			query := "SELECT 'orders' AS label, 42 AS value"
			definition, err := Parse([]byte(fmt.Sprintf("version: 1\nname: Charts\npanels:\n  - title: Chart\n    display_type: %s\n    query: %s\n    settings: {min: 0, max: 200, unit: requests}\n    position: 0\n", kind, query)))
			if err != nil {
				t.Fatal(err)
			}
			dashboard, err := definition.Dashboard("charts")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Marshal(dashboard)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Parse(encoded)
			if err != nil || restored.Panels[0].DisplayType != kind {
				t.Fatalf("roundtrip: %s, %v", encoded, err)
			}
			columns := []string{"LABEL", "VALUE"}
			if kind == "gauge" {
				columns = []string{"VALUE"}
			}
			if err := ValidatePanelResult(kind, columns); err != nil {
				t.Fatal(err)
			}
			if err := ValidatePanelResult(kind, []string{"label"}); err == nil {
				t.Fatal("accepted missing value")
			}
			if kind != "gauge" && ValidatePanelResult(kind, []string{"value"}) == nil {
				t.Fatal("accepted missing label")
			}
		})
	}
}
