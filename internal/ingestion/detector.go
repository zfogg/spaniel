package ingestion

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/goroutine"
	"github.com/zfogg/spaniel/internal/ingestion/linter"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

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
}

// runDetectors runs all registered detectors on the trace identified by traceID,
// persists any issues, and broadcasts WS events for each new finding.
func runDetectors(traceID string, store *storage.DB, hub *ws.Hub, tracer trace.Tracer) {
	_, dspan := tracer.Start(context.Background(), "runDetectors",
		trace.WithAttributes(attribute.String("trace_id", traceID)))
	defer dspan.End()

	spans, err := store.GetTrace(traceID)
	if err != nil {
		slog.Warn("trace detector read failed", "trace_id", traceID, "error", err)
		return
	}
	if len(spans) == 0 {
		return
	}
	sessID := store.ActiveSessionID()
	now := time.Now().UnixNano()
	linter.PersistStorageQueryNameLints(traceID, store, spans, now)

	for _, d := range registry {
		if !detectorSupportsTrace(d, spans) {
			continue
		}
		for _, issue := range d.Analyze(traceID, sessID, spans, now) {
			if err := store.UpsertTraceIssue(issue); err == nil {
				telemetry.Catalog().RecordDetectorIssue(context.Background(), issue.Kind)
				goroutine.Go(func() {
					hub.Broadcast(ws.NewIssueEvent(&ws.IssuePayload{
						TraceID:     issue.TraceID,
						Kind:        issue.Kind,
						Fingerprint: issue.Fingerprint,
						Count:       issue.Count,
						WastedNs:    issue.WastedNs,
					}))
				}, "subsystem", "detectors")
			} else {
				slog.Warn("trace detector finding write failed", "trace_id", traceID, "kind", issue.Kind, "error", err)
			}
		}
	}
}

// Complete-tree heuristics cannot infer missing work or total repetition from
// representative span samples. Error/slow-span checks still use the evidence
// actually received. Clients opt in with a boolean on their operation root.
func detectorSupportsTrace(d Detector, spans []*storage.Span) bool {
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
