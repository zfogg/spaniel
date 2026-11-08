package ingestion

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
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

var detectorTraceLocks [64]sync.Mutex

// runDetectors runs all registered detectors on the trace identified by traceID,
// persists any issues, and broadcasts WS events for each new finding.
func runDetectors(traceID, sessionID string, store *storage.DB, hub *ws.Hub, tracer trace.Tracer) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(traceID))
	lock := &detectorTraceLocks[h.Sum32()%uint32(len(detectorTraceLocks))]
	lock.Lock()
	defer lock.Unlock()
	_, dspan := tracer.Start(context.Background(), "runDetectors",
		trace.WithAttributes(attribute.String("trace_id", traceID)))
	defer dspan.End()

	spans, err := store.GetTraceInSessionForDetection(traceID, sessionID)
	if err != nil {
		slog.Warn("trace detector read failed", "trace_id", traceID, "error", err)
		return
	}
	if len(spans) == 0 {
		return
	}
	sessID := sessionID
	now := time.Now().UnixNano()
	linter.PersistStorageQueryNameLints(traceID, store, spans, now)
	linter.PersistDuplicateSpanLints(traceID, store, spans, now)
	linter.PersistChildStartsBeforeParentLints(traceID, store, spans, now)

	for _, d := range detectors.Registry() {
		if !detectors.SupportsTrace(d, spans) {
			continue
		}
		issues := d.Analyze(traceID, sessID, spans, now)
		succeeded := true
		for _, issue := range issues {
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
				succeeded = false
				slog.Warn("trace detector finding write failed", "trace_id", traceID, "kind", issue.Kind, "error", err)
			}
		}
		if d.Kind() == "timeout" && succeeded {
			ids := make([]string, 0, len(issues))
			for _, issue := range issues {
				ids = append(ids, issue.ID)
			}
			if err := store.DeleteObsoleteTraceIssues(traceID, sessionID, d.Kind(), ids); err != nil {
				slog.Warn("trace detector reconciliation failed", "trace_id", traceID, "kind", d.Kind(), "error", err)
			}
		}
	}
}
