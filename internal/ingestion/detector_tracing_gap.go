package ingestion

import (
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/otel/trace"
)

const tracingGapThreshold = 50 * time.Millisecond

// TracingGapDetector checks application-operation coverage. Outbound client
// duration includes transport and remote waiting, which child application
// spans cannot be expected to cover.
type TracingGapDetector struct{}

func (*TracingGapDetector) Kind() string { return "tracing_gap" }

func (*TracingGapDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	// Build parent → children index.
	children := map[string][]*storage.Span{}
	for _, s := range spans {
		if s.ParentSpanID != "" {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], s)
		}
	}

	var issues []*storage.TraceIssue
	for _, s := range spans {
		// CLIENT spans measure the entire remote call, including network time.
		// A remote SERVER child also has its own host clock. Subtracting its
		// duration from the client is not a measure of missing local work.
		// Continue analyzing server/internal descendants independently.
		if s.Kind == int(trace.SpanKindClient) {
			continue
		}
		// HTTP 101 transfers this handler to a long-lived upgraded protocol.
		// Time between WebSocket messages is connection idle time, not missing
		// request work. Analyze its child operations normally below.
		if isHTTPSpan(s) && (extractAttrFloat64(s.Attributes, "http.response.status_code") == 101 || extractAttrFloat64(s.Attributes, "http.status_code") == 101) {
			continue
		}
		kids := children[s.SpanID]
		if len(kids) == 0 {
			continue
		}
		parentDur := s.EndNs - s.StartNs
		if parentDur <= 0 {
			continue
		}
		covered := coveredNs(s.StartNs, s.EndNs, kids)
		gap := parentDur - covered
		if gap <= int64(tracingGapThreshold) {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "tracing_gap", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "tracing_gap",
			Fingerprint:   s.Name,
			Count:         1,
			WastedNs:      gap,
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
