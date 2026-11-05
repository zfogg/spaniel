package detectors

import "github.com/zfogg/spaniel/internal/storage"

// Detector analyzes a trace and returns findings. Implementations must not mutate spans.
type Detector interface {
	Kind() string
	Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue
}

// Registry returns the trace detectors in evaluation order.
func Registry() []Detector {
	return []Detector{&N1Detector{}, &SlowDBDetector{}, &ChattyHTTPDetector{}, &LargePayloadDetector{}, &CacheMissStormDetector{}, &TracingGapDetector{}, &ErrorChainDetector{}, &SerialPromiseDetector{}}
}

// SupportsTrace applies the partial-trace policy. Complete-tree heuristics cannot infer missing work or total repetition from
// representative span samples. Error/slow-span checks still use the evidence
// actually received. Clients opt in with a boolean on their operation root.
func SupportsTrace(d Detector, spans []*storage.Span) bool {
	for _, s := range spans {
		if partial, ok := extractAttrBool(s.Attributes, "spaniel.trace.partial"); ok && partial {
			switch d.Kind() {
			case "n_plus_one", "tracing_gap", "serial_promise":
				return false
			}
		}
	}
	return true
}
