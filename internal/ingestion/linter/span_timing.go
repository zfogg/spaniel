package linter

import (
	"context"
	"fmt"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

const ChildStartsBeforeParentRule = "span.starts_before_parent"

// ChildStartsBeforeParentLints finds timestamp inversions in a trace tree. Span
// arrival order and sibling overlap are both valid, but a child cannot begin
// before its parent without the two spans using inconsistent clocks.
func ChildStartsBeforeParentLints(spans []*storage.Span, now int64) []*storage.LintWarning {
	byID := make(map[string]*storage.Span, len(spans))
	for _, span := range spans {
		if span.SpanID != "" {
			byID[span.SpanID] = span
		}
	}

	var warnings []*storage.LintWarning
	for _, span := range spans {
		if span.ParentSpanID == "" || span.StartNs <= 0 {
			continue
		}
		parent := byID[span.ParentSpanID]
		if parent == nil || parent.StartNs <= 0 || span.StartNs >= parent.StartNs {
			continue
		}
		warnings = append(warnings, &storage.LintWarning{
			SpanID:    span.SpanID,
			TraceID:   span.TraceID,
			SessionID: span.SessionID,
			RuleID:    ChildStartsBeforeParentRule,
			Message:   fmt.Sprintf("span starts %s before its parent; parent and child must use a consistent clock", time.Duration(parent.StartNs-span.StartNs)),
			Severity:  "warning",
			CreatedAt: now,
		})
	}
	return warnings
}

func PersistChildStartsBeforeParentLints(traceID string, store *storage.DB, spans []*storage.Span, now int64) {
	warnings := ChildStartsBeforeParentLints(spans, now)
	if count, err := store.InsertMissingTraceLints(traceID, ChildStartsBeforeParentRule, warnings); err == nil && count > 0 {
		for range count {
			telemetry.Catalog().RecordLintWarning(context.Background(), ChildStartsBeforeParentRule, "warning")
		}
	}
}
