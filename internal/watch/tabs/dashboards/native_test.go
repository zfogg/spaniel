package dashboards

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

func preview(t *testing.T, raw string) apigen.QueryPreview {
	t.Helper()
	var result apigen.QueryPreview
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNativeSignedBarsPieSharesAndExactTotal(t *testing.T) {
	data := preview(t, `{"rows":[{"label":"credit","value":6},{"group_value":"debit","value":-2},{"label":"zero","value":0},{"label":"unknown","value":null}]}`)
	bar := strings.Join(renderPanel("bar", `{"unit":"requests"}`, data, 60, 20, false), "\n")
	for _, text := range []string{"Signed bars", "credit  6 requests", "debit  -2 requests", "zero  0 requests", "│", "█", "1 null/invalid"} {
		if !strings.Contains(bar, text) {
			t.Fatalf("bar omitted %q: %s", text, bar)
		}
	}
	if pie := strings.Join(renderPanel("pie", "{}", data, 60, 20, false), "\n"); !strings.Contains(pie, "non-negative") || strings.Contains(pie, "%") {
		t.Fatal("negative pie shares", pie)
	}
	data = preview(t, `{"rows":[{"label":"small","value":1},{"label":"large","value":3},{"label":"zero","value":0}]}`)
	pie := strings.Join(renderPanel("pie", "{}", data, 60, 20, false), "\n")
	for _, text := range []string{"total 4", "1. large", "75.0%", "25.0%", "0.0%"} {
		if !strings.Contains(pie, text) {
			t.Fatal(text, pie)
		}
	}
	large := preview(t, `{"rows":[{"label":"a","value":9007199254740993},{"label":"b","value":9007199254740993}]}`)
	text := strings.Join(renderPanel("pie", "{}", large, 60, 20, false), "\n")
	if !strings.Contains(text, "total 18014398509481986") || strings.Count(text, "9007199254740993") != 2 {
		t.Fatal("pie rounded actual query values", text)
	}
	large = preview(t, `{"rows":[{"label":"a","value":1e308},{"label":"b","value":1e308}]}`)
	text = strings.Join(renderPanel("pie", "{}", large, 60, 20, false), "\n")
	if strings.Contains(text, "Inf") || strings.Contains(text, "unavailable") || strings.Count(text, "50.0%") != 2 {
		t.Fatal("pie total overflow", text)
	}
	zeros := preview(t, `{"rows":[{"label":"zero","value":0}]}`)
	if text := strings.Join(renderPanel("pie", "{}", zeros, 60, 20, false), "\n"); !strings.Contains(text, "No positive") {
		t.Fatal(text)
	}
}

func TestNativeGaugeRangeClampsOnlyMeterAndHandlesThresholds(t *testing.T) {
	data := preview(t, `{"rows":[{"value":150}]}`)
	text := strings.Join(renderPanel("gauge", `{"min":0,"max":100,"unit":"jobs","thresholds":[{"label":"warning","value":80}]}`, data, 40, 20, false), "\n")
	for _, want := range []string{"Value: 150 jobs", "Range: 0 → 100 jobs", "Out of range:", "warning: 80 jobs · reached"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Count(text, "█") != 38 {
		t.Fatal("visual fill not clamped", text)
	}
	for _, config := range []string{`{"min":10,"max":10}`, `{"min":20,"max":10}`, `{"min":"bad"}`, `{"min":-1e308,"max":1e308}`, `{broken`} {
		if text := strings.Join(renderPanel("gauge", config, data, 60, 20, false), "\n"); !strings.Contains(text, "Invalid") || strings.Contains(text, "█") {
			t.Fatal("invalid gauge rendered", config, text)
		}
	}
	for _, kind := range []string{"bar", "pie", "gauge", "heatmap", "entity_list"} {
		if text := strings.Join(renderPanel(kind, "{}", apigen.QueryPreview{}, 30, 10, false), "\n"); !strings.Contains(text, "No telemetry") {
			t.Fatal(kind, text)
		}
		if text := strings.Join(renderPanel(kind, "{}", preview(t, `{"rows":[{"value":null}]}`), 30, 10, false), "\n"); !strings.Contains(text, "No ") {
			t.Fatal(kind, text)
		}
	}
}

func TestNativeHeatmapSparseZeroDuplicatesInvalidAndBounds(t *testing.T) {
	data := preview(t, `{"columns":["timestamp_ns","bucket_ms","value"],"rows":[{"timestamp_ns":1793631000000000001,"bucket_ms":10,"value":0},{"timestamp_ns":1793631060000000001,"bucket_ms":20,"value":8},{"timestamp_ns":1793631000000000001,"bucket_ms":20,"value":2},{"timestamp_ns":1793631000000000001,"bucket_ms":20,"value":3},{"timestamp_ns":null,"bucket_ms":20,"value":2},{"timestamp_ns":1793631060000000001,"bucket_ms":30,"value":-1}]}`)
	text := strings.Join(renderPanel("heatmap", "{}", data, 100, 20, false), "\n")
	for _, want := range []string{"UTC time buckets", "10 ms", "20 ms", " 0 ", " · ", "█", "0 → 8", "14:50:00.000 UTC", "2 invalid/negative"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Contains(text, "30 ms") {
		t.Fatal("invalid cell created an axis", text)
	}
	if narrow := strings.Join(renderPanel("heatmap", "{}", data, 20, 12, false), "\n"); !strings.Contains(narrow, "grid exceeds") || !strings.Contains(narrow, "bucket_ms") {
		t.Fatal(narrow)
	}
	malformed := apigen.QueryPreview{Rows: []map[string]any{{"x": math.Inf(1), "y": 1, "value": 2}, {"x": 1, "y": math.NaN(), "value": 2}, {"x": 1, "y": 2, "value": math.NaN()}}}
	if bad := strings.Join(renderPanel("heatmap", "{}", malformed, 80, 12, false), "\n"); !strings.Contains(bad, "No valid heatmap") {
		t.Fatal(bad)
	}
	many := apigen.QueryPreview{}
	for i := 0; i < 65; i++ {
		many.Rows = append(many.Rows, map[string]any{"x": i, "y": 1, "value": 1})
	}
	if text := strings.Join(renderPanel("heatmap", "{}", many, 300, 20, false), "\n"); !strings.Contains(text, "grid exceeds") {
		t.Fatal("unbounded heatmap", text)
	}
	// All native layouts retain data after actual model wrapping on tiny widths.
	for _, width := range []int{1, 18, 40, 80} {
		for _, kind := range []string{"bar", "pie", "gauge", "entity_list", "heatmap"} {
			for _, line := range renderPanel(kind, "{}", data, width, 12, false) {
				for _, wrapped := range wrapUI([]uiLine{plain(line)}, width) {
					if ansi.StringWidth(wrapped) > width {
						t.Fatal(kind, width, wrapped)
					}
				}
			}
		}
	}
}
