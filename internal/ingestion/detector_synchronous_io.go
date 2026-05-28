package ingestion

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/storage"
)

const syncIOMinDB = 3

// SynchronousIODetector fires when a server-kind GET handler executes ≥ 3 DB
// spans inline (blocking I/O on a latency-sensitive path).
type SynchronousIODetector struct{}

func (*SynchronousIODetector) Kind() string { return "synchronous_io" }

func (*SynchronousIODetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	var issues []*storage.TraceIssue
	for _, s := range spans {
		// Server span (Kind 2 = SPAN_KIND_SERVER).
		if s.Kind != 2 {
			continue
		}
		method := extractAttrString(s.Attributes, "http.method")
		if method == "" {
			method = extractAttrString(s.Attributes, "http.request.method")
		}
		if method != "GET" {
			continue
		}

		// Count direct DB children.
		var dbKids []*storage.Span
		var dbNs int64
		for _, c := range childrenOf(s.SpanID, spans) {
			if isDBSpan(c) {
				dbKids = append(dbKids, c)
				dbNs += c.EndNs - c.StartNs
			}
		}
		if len(dbKids) < syncIOMinDB {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "synchronous_io", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "synchronous_io",
			Fingerprint:   fmt.Sprintf("%s (%d db calls)", s.Name, len(dbKids)),
			Count:         len(dbKids),
			WastedNs:      dbNs,
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
