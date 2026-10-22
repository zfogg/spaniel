package linter

import (
	"context"
	"fmt"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

const DuplicateSpanRule = "span.duplicate_instrumentation"

// DuplicateSpanLints detects the characteristic output of installing one
// interceptor twice: same service, name, parent, start, and end with distinct IDs.
func DuplicateSpanLints(spans []*storage.Span, now int64) []*storage.LintWarning {
	seen := map[string]*storage.Span{}
	result := []*storage.LintWarning{}
	for _, span := range spans {
		key := fmt.Sprintf("%s|%s|%s|%d|%d|%s", span.ServiceName, span.Name, span.ParentSpanID, span.StartNs, span.EndNs, span.Attributes)
		if prior := seen[key]; prior != nil && prior.SpanID != span.SpanID {
			result = append(result, &storage.LintWarning{SpanID: span.SpanID, TraceID: span.TraceID, SessionID: span.SessionID, RuleID: DuplicateSpanRule, Message: "duplicate identically timed span; instrumentation may be installed twice", Severity: "warning", CreatedAt: now})
		} else {
			seen[key] = span
		}
	}
	return result
}

func PersistDuplicateSpanLints(traceID string, store *storage.DB, spans []*storage.Span, now int64) {
	warnings := DuplicateSpanLints(spans, now)
	if count, err := store.InsertMissingTraceLints(traceID, DuplicateSpanRule, warnings); err == nil && count > 0 {
		for range count {
			telemetry.Catalog().RecordLintWarning(context.Background(), DuplicateSpanRule, "warning")
		}
	}
}
