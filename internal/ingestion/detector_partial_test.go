package ingestion

import (
	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/storage"
	"testing"
)

func TestPartialTraceSkipsWholeTreeHeuristicsButRetainsFailures(t *testing.T) {
	partial := []*storage.Span{{Attributes: `{"spaniel.trace.partial":true}`}}
	complete := []*storage.Span{{Attributes: `{}`}}
	for _, d := range []detectors.Detector{&detectors.N1Detector{}, &detectors.TracingGapDetector{}, &detectors.SerialPromiseDetector{}} {
		if detectors.SupportsTrace(d, partial) {
			t.Fatalf("%s accepted incomplete detail", d.Kind())
		}
		if !detectors.SupportsTrace(d, complete) {
			t.Fatalf("%s stopped analysing complete traces", d.Kind())
		}
	}
	for _, d := range []detectors.Detector{&detectors.SlowDBDetector{}, &detectors.ErrorChainDetector{}} {
		if !detectors.SupportsTrace(d, partial) {
			t.Fatalf("%s was disabled by sampling", d.Kind())
		}
	}
}
