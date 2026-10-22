package linter

import (
	"github.com/zfogg/spaniel/internal/storage"
	"testing"
)

func TestDuplicateSpanLints(t *testing.T) {
	spans := []*storage.Span{{TraceID: "trace", SpanID: "one", ParentSpanID: "parent", ServiceName: "ratio-worker", Name: "ratio.temporal.activity.Refresh", StartNs: 10, EndNs: 20}, {TraceID: "trace", SpanID: "two", ParentSpanID: "parent", ServiceName: "ratio-worker", Name: "ratio.temporal.activity.Refresh", StartNs: 10, EndNs: 20}}
	warnings := DuplicateSpanLints(spans, 30)
	if len(warnings) != 1 || warnings[0].RuleID != DuplicateSpanRule {
		t.Fatalf("warnings=%+v", warnings)
	}
}
