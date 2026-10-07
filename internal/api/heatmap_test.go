package api

import "testing"

func TestHeatmapResultShape(t *testing.T) {
	for _, columns := range [][]string{{"timestamp_ns", "bucket_ms", "value"}, {"x", "y", "value"}, {"x", "value"}} {
		if err := validatePanelResult("heatmap", columns); err != nil {
			t.Fatal(err)
		}
	}
	if validatePanelResult("heatmap", []string{"bucket_ms", "value"}) == nil {
		t.Fatal("missing time/x accepted")
	}
	if validatePanelResult("heatmap", []string{"timestamp_ns", "bucket_ms"}) == nil {
		t.Fatal("missing count accepted")
	}
}
