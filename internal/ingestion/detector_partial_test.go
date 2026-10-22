package ingestion

import (
	"github.com/zfogg/spaniel/internal/storage"
	"testing"
)

func TestPartialTraceSkipsWholeTreeHeuristicsButRetainsFailures(t *testing.T) {
	partial := []*storage.Span{{Attributes: `{"spaniel.trace.partial":true}`}}
	complete := []*storage.Span{{Attributes: `{}`}}
	for _, d := range []Detector{&N1Detector{}, &TracingGapDetector{}, &SerialPromiseDetector{}} {
		if detectorSupportsTrace(d, partial) {
			t.Fatalf("%s accepted incomplete detail", d.Kind())
		}
		if !detectorSupportsTrace(d, complete) {
			t.Fatalf("%s stopped analysing complete traces", d.Kind())
		}
	}
	for _, d := range []Detector{&SlowDBDetector{}, &ErrorChainDetector{}} {
		if !detectorSupportsTrace(d, partial) {
			t.Fatalf("%s was disabled by sampling", d.Kind())
		}
	}
}
