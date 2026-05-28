package ingestion

import (
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

const slowDBThreshold = 250 * time.Millisecond

// SlowDBDetector fires when a single DB span exceeds 250 ms.
type SlowDBDetector struct{}

func (*SlowDBDetector) Kind() string { return "slow_db" }

func (*SlowDBDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	var issues []*storage.TraceIssue
	for _, s := range spans {
		if !isDBSpan(s) {
			continue
		}
		dur := s.EndNs - s.StartNs
		if dur <= int64(slowDBThreshold) {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "slow_db", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "slow_db",
			Fingerprint:   s.Name,
			Count:         1,
			WastedNs:      dur - int64(slowDBThreshold),
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
