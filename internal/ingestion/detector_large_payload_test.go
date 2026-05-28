package ingestion

import (
	"fmt"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func payloadSpan(id string, bytes int) *storage.Span {
	return &storage.Span{
		SpanID:     id,
		Attributes: fmt.Sprintf(`{"http.response_content_length":%d}`, bytes),
		SessionID:  "sess",
	}
}

func TestLargePayload_BelowThreshold(t *testing.T) {
	d := &LargePayloadDetector{}
	spans := []*storage.Span{payloadSpan("s1", 500_000)} // 500 KB
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("500 KB should not fire, got %d", len(got))
	}
}

func TestLargePayload_AtThreshold_NoFire(t *testing.T) {
	d := &LargePayloadDetector{}
	spans := []*storage.Span{payloadSpan("s1", 1<<20)} // exactly 1 MB
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("exactly 1 MB should not fire, got %d", len(got))
	}
}

func TestLargePayload_AboveThreshold(t *testing.T) {
	d := &LargePayloadDetector{}
	spans := []*storage.Span{payloadSpan("s1", 2_000_000)} // 2 MB
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 2 MB, got %d", len(got))
	}
	if got[0].Kind != "large_payload" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
}

func TestLargePayload_AlternativeAttr(t *testing.T) {
	d := &LargePayloadDetector{}
	spans := []*storage.Span{{
		SpanID:     "s1",
		Attributes: `{"http.response_body_size":5000000}`,
		SessionID:  "sess",
	}}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 1 {
		t.Errorf("expected 1 issue via http.response_body_size, got %d", len(got))
	}
}
