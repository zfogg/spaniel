package ingestion

import (
	"github.com/zfogg/spaniel/internal/storage"
)

// ErrorChainDetector fires when a child span errored but its parent did not
// propagate the error (parent.status_code != 2 while child.status_code == 2).
type ErrorChainDetector struct{}

func (*ErrorChainDetector) Kind() string { return "error_chain" }

func (*ErrorChainDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	byID := make(map[string]*storage.Span, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = s
	}

	var issues []*storage.TraceIssue
	for _, s := range spans {
		if s.StatusCode != 2 {
			continue
		}
		if s.ParentSpanID == "" {
			continue
		}
		parent := byID[s.ParentSpanID]
		if parent == nil || parent.StatusCode == 2 {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "error_chain", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "error_chain",
			Fingerprint:   s.Name,
			Count:         1,
			WastedNs:      0,
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
