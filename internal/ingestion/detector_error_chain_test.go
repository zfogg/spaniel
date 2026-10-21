package ingestion

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestErrorChain_ChildErrorParentOK(t *testing.T) {
	spans := []*storage.Span{
		{SpanID: "root", ParentSpanID: "", StatusCode: 1, SessionID: "sess"},
		{SpanID: "child", ParentSpanID: "root", StatusCode: 2, SessionID: "sess"},
	}
	d := &ErrorChainDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(got))
	}
	if got[0].Kind != "error_chain" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	if got[0].ExampleSpanID != "child" {
		t.Errorf("ExampleSpanID = %q, want %q", got[0].ExampleSpanID, "child")
	}
}

func TestErrorChain_BothError_NoFire(t *testing.T) {
	// When parent also errors, the chain is propagated — not a silent swallow.
	spans := []*storage.Span{
		{SpanID: "root", ParentSpanID: "", StatusCode: 2, SessionID: "sess"},
		{SpanID: "child", ParentSpanID: "root", StatusCode: 2, SessionID: "sess"},
	}
	d := &ErrorChainDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("both-error should not fire, got %d", len(got))
	}
}

func TestErrorChain_AllOK_NoFire(t *testing.T) {
	spans := []*storage.Span{
		{SpanID: "root", ParentSpanID: "", StatusCode: 1, SessionID: "sess"},
		{SpanID: "child", ParentSpanID: "root", StatusCode: 1, SessionID: "sess"},
	}
	d := &ErrorChainDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("all-OK should not fire, got %d", len(got))
	}
}

func TestErrorChain_MultipleChildren(t *testing.T) {
	// Two errored children whose parent doesn't propagate → 2 issues.
	spans := []*storage.Span{
		{SpanID: "root", ParentSpanID: "", StatusCode: 1, SessionID: "sess"},
		{SpanID: "c1", ParentSpanID: "root", StatusCode: 2, SessionID: "sess"},
		{SpanID: "c2", ParentSpanID: "root", StatusCode: 2, SessionID: "sess"},
	}
	d := &ErrorChainDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 2 {
		t.Errorf("expected 2 issues, got %d", len(got))
	}
}

func TestErrorChainExplicitCallerTerminationOnlyExemptsThatBoundary(t *testing.T) {
	for _, attrs := range []struct {
		value    string
		expected int
	}{
		{`{"operation.termination":"caller_cancelled","error.type":"context_canceled"}`, 0},
		{`{"error.type":"context_canceled"}`, 1},
		{`{"operation.termination":"caller_cancelled"}`, 1},
		{`{"operation.termination":"caller_cancelled","error.type":"context_deadline_exceeded"}`, 1},
		{`{"operation.termination":"caller_cancelled","error.type":"database_failed"}`, 1},
		{`{"operation.termination":true,"error.type":"context_canceled"}`, 1},
	} {
		spans := []*storage.Span{
			{SpanID: "root", StatusCode: 0},
			{SpanID: "boundary", ParentSpanID: "root", StatusCode: 2, Attributes: attrs.value},
			{SpanID: "sibling", ParentSpanID: "root", StatusCode: 2},
			{SpanID: "inside", ParentSpanID: "boundary", StatusCode: 0},
			{SpanID: "failed_descendant", ParentSpanID: "inside", StatusCode: 2},
		}
		got := (&ErrorChainDetector{}).Analyze("trace", "session", spans, 0)
		if len(got) != attrs.expected+2 {
			t.Fatalf("attrs=%s issues=%+v", attrs.value, got)
		}
		if spans[1].StatusCode != 2 {
			t.Fatal("detector changed retained error status")
		}
		found := map[string]bool{}
		for _, issue := range got {
			found[issue.ExampleSpanID] = true
		}
		if !found["sibling"] || !found["failed_descendant"] {
			t.Fatal("caller marker suppressed unrelated failures")
		}
	}
}
