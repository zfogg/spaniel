package metrics

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

func TestAttributeTablePreservesTypesNumbersContainersAndWrappedValues(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "0")
	var attrs map[string]any
	if err := jsonDecode(`{"count":9007199254740993,"decimal":1.234567890123456789,"enabled":true,"missing":null,"deployment":{"region":"us-east","replicas":3},"tags":["api",false,7]}`, &attrs); err != nil {
		t.Fatal(err)
	}
	if attrs["count"].(json.Number).String() != "9007199254740993" {
		t.Fatal("attribute decoder lost integer precision")
	}
	lines := attributeTable("Series labels", attrs, 110)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"KEY", "TYPE", "VALUE", "9007199254740993", "1.234567890123456789", "boolean", "null", `deployment["region"]`, `tags[1]`, "array", "object"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, `{"count"`) {
		t.Fatal("attribute map rendered as JSON")
	}
	long := "BEGIN_" + strings.Repeat("very-long-value-", 10) + "_END"
	for _, width := range []int{12, 32, 60} {
		lines = attributeTable("Telemetry attributes", map[string]any{"long.key": long}, width)
		pieces := []string{}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("attribute width %d overflow %q", width, line)
			}
			if width >= 40 {
				columns := strings.SplitN(line, " │ ", 3)
				if len(columns) == 3 {
					pieces = append(pieces, columns[2])
				}
			} else {
				pieces = append(pieces, line)
			}
		}
		if !strings.Contains(strings.Join(pieces, ""), long) {
			t.Fatalf("wrapped attribute value lost at width %d", width)
		}
	}
}
func TestMetricLabelAndScopeAttributesAreScrollableTables(t *testing.T) {
	env := testEnv(t)
	env.Owner = "detail:attributes"
	d := newDetail(env, apigen.MetricCatalogEntry{Name: "cpu", ServiceName: "api", Type: "gauge"})
	d.loaded = true
	d.data = seriesData{Type: "gauge", Series: []labeledSeries{{Key: "one", Attributes: map[string]any{"http.route": "/cart"}, Points: []sample{{Timestamp: d.now.UnixNano(), Value: 10, ScopeName: "sdk", ScopeAttributes: map[string]any{"enabled": true, "worker.id": json.Number("9007199254740993")}}}}}}
	d.reconcile()
	view := d.View()
	if !strings.Contains(view, "Series labels") || !strings.Contains(view, "KEY") || !strings.Contains(view, "TYPE") || strings.Contains(view, `{"http.route"`) {
		t.Fatal("series label map is not a table")
	}
	d.Update(tea.KeyMsg{Type: tea.KeyEnd})
	view = d.View()
	for _, want := range []string{"Scope attributes", "worker.id", "9007199254740993", "boolean"} {
		if !strings.Contains(view, want) {
			t.Errorf("full attribute table not reachable by scrolling: missing %q", want)
		}
	}
}

func TestScopeAttributesRemainVisibleWithoutScopeName(t *testing.T) {
	text := strings.Join(sampleDescription(sample{ScopeAttributes: map[string]any{"sdk.enabled": true}}, "gauge", 100), "\n")
	if !strings.Contains(text, "Scope attributes") || !strings.Contains(text, "sdk.enabled") {
		t.Fatal("unnamed scope lost its attributes")
	}
}
