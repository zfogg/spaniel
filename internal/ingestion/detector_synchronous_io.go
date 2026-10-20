package ingestion

import (
	"fmt"
	"sort"

	"github.com/zfogg/spaniel/internal/storage"
)

const syncIOMinDB = 3

// SynchronousIODetector reports at least three observed serial DB phases in a
// GET handler. Overlapping calls form one phase, not separate round trips.
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

		type interval struct{ start, end int64 }
		var calls []interval
		for _, c := range childrenOf(s.SpanID, spans) {
			if !isDBSpan(c) || (s.ServiceName != "" && c.ServiceName != "" && s.ServiceName != c.ServiceName) {
				continue
			}
			// Only time within this handler can represent its blocking I/O.
			start, end := max(s.StartNs, c.StartNs), min(s.EndNs, c.EndNs)
			if start < end {
				calls = append(calls, interval{start, end})
			}
		}
		if len(calls) < syncIOMinDB {
			continue
		}
		sort.Slice(calls, func(i, j int) bool { return calls[i].start < calls[j].start })
		phases := 1
		var dbNs int64
		current := calls[0]
		for _, call := range calls[1:] {
			if call.start < current.end {
				current.end = max(current.end, call.end)
				continue
			}
			dbNs += current.end - current.start
			phases++
			current = call
		}
		dbNs += current.end - current.start
		if phases < syncIOMinDB {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "synchronous_io", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "synchronous_io",
			Fingerprint:   fmt.Sprintf("%s (%d serial DB phases; %d calls)", s.Name, phases, len(calls)),
			Count:         phases,
			WastedNs:      dbNs,
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
