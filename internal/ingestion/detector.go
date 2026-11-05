package ingestion

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/goroutine"
	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/ingestion/linter"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

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
	linter.PersistDuplicateSpanLints(traceID, store, spans, now)
	linter.PersistChildStartsBeforeParentLints(traceID, store, spans, now)

	for _, d := range detectors.Registry() {
		if !detectors.SupportsTrace(d, spans) {
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
