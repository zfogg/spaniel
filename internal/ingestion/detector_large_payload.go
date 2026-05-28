package ingestion

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/storage"
)

const largePayloadThreshold = 1 << 20 // 1 MB

// LargePayloadDetector fires when http.response_content_length exceeds 1 MB.
type LargePayloadDetector struct{}

func (*LargePayloadDetector) Kind() string { return "large_payload" }

func (*LargePayloadDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	var issues []*storage.TraceIssue
	for _, s := range spans {
		size := extractAttrFloat64(s.Attributes, "http.response_content_length")
		if size == 0 {
			size = extractAttrFloat64(s.Attributes, "http.response_body_size")
		}
		if size == 0 {
			size = extractAttrFloat64(s.Attributes, "http.response.body.size")
		}
		if size <= largePayloadThreshold {
			continue
		}
		issues = append(issues, &storage.TraceIssue{
			ID:            issueID(traceID, "large_payload", s.SpanID),
			TraceID:       traceID,
			SessionID:     sessionID,
			Kind:          "large_payload",
			Fingerprint:   fmt.Sprintf("%.0f bytes", size),
			Count:         1,
			WastedNs:      int64(size),
			ParentSpanID:  s.ParentSpanID,
			ExampleSpanID: s.SpanID,
			CreatedAt:     now,
		})
	}
	return issues
}
