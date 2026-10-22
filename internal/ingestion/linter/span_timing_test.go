package linter

import (
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestChildStartsBeforeParentLints(t *testing.T) {
	spans := []*storage.Span{
		{TraceID: "trace", SpanID: "workflow", StartNs: 1_000_000_000, EndNs: 2_000_000_000},
		{TraceID: "trace", SpanID: "schedule", ParentSpanID: "workflow", Name: "ratio.temporal.activity.schedule.Lifecycle", StartNs: 998_900_800, EndNs: 1_000_000_100},
		{TraceID: "trace", SpanID: "execute", ParentSpanID: "schedule", Name: "ratio.temporal.activity.execute.Lifecycle", StartNs: 1_000_000_100, EndNs: 2_000_000_000},
	}

	warnings := ChildStartsBeforeParentLints(spans, 123)
	if len(warnings) != 1 {
		t.Fatalf("warnings=%+v", warnings)
	}
	if warning := warnings[0]; warning.RuleID != ChildStartsBeforeParentRule || warning.SpanID != "schedule" || !strings.Contains(warning.Message, "1.0992ms") {
		t.Fatalf("warning=%+v", warning)
	}
}

func TestChildStartsBeforeParentLints_AllowsValidAndIncompleteTrees(t *testing.T) {
	spans := []*storage.Span{
		{TraceID: "trace", SpanID: "parent", StartNs: 1_000, EndNs: 2_000},
		{TraceID: "trace", SpanID: "child", ParentSpanID: "parent", StartNs: 1_100, EndNs: 1_200},
		{TraceID: "trace", SpanID: "late", ParentSpanID: "not-exported", StartNs: 900, EndNs: 1_000},
	}
	if warnings := ChildStartsBeforeParentLints(spans, 123); len(warnings) != 0 {
		t.Fatalf("warnings=%+v", warnings)
	}
}
