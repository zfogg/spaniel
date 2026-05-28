package ingestion

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/goroutine"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

var detectorIssuesCounter metric.Int64Counter

func init() {
	detectorIssuesCounter, _ = otel.Meter("spaniel/ingestion").Int64Counter(
		"spaniel.detector.issues_found",
		metric.WithDescription("Trace issues detected by post-ingestion detectors"),
		metric.WithUnit("{issue}"),
	)
}

// Detector analyzes a trace's spans and returns any detected issues.
// Each implementation lives in its own detector_*.go file.
type Detector interface {
	Kind() string
	Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue
}

// registry is the ordered list of detectors run on every trace.
var registry = []Detector{
	&N1Detector{},
	&SlowDBDetector{},
	&ChattyHTTPDetector{},
	&LargePayloadDetector{},
	&CacheMissStormDetector{},
	&TracingGapDetector{},
	&ErrorChainDetector{},
	&SerialPromiseDetector{},
	&SynchronousIODetector{},
}

// runDetectors runs all registered detectors on the trace identified by traceID,
// persists any issues, and broadcasts WS events for each new finding.
func runDetectors(traceID string, store *storage.DB, hub *ws.Hub) {
	_, dspan := otel.Tracer("spaniel/ingestion").Start(context.Background(), "runDetectors",
		trace.WithAttributes(attribute.String("trace_id", traceID)))
	defer dspan.End()

	spans, err := store.GetTrace(traceID)
	if err != nil || len(spans) == 0 {
		return
	}
	sessID := store.ActiveSessionID()
	now := time.Now().UnixNano()

	for _, d := range registry {
		for _, issue := range d.Analyze(traceID, sessID, spans, now) {
			if err := store.UpsertTraceIssue(issue); err == nil {
				detectorIssuesCounter.Add(context.Background(), 1,
					metric.WithAttributes(attribute.String("kind", issue.Kind)))
				goroutine.Go(func() {
					hub.Broadcast(ws.NewIssueEvent(&ws.IssuePayload{
						TraceID:     issue.TraceID,
						Kind:        issue.Kind,
						Fingerprint: issue.Fingerprint,
						Count:       issue.Count,
						WastedNs:    issue.WastedNs,
					}))
				}, "subsystem", "detectors")
			}
		}
	}
}
