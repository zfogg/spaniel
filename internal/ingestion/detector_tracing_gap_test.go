package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func gapSpan(id, parentID string, startMs, endMs int) *storage.Span {
	return &storage.Span{
		SpanID:      id,
		ParentSpanID: parentID,
		StartNs:     int64(startMs) * int64(time.Millisecond),
		EndNs:       int64(endMs) * int64(time.Millisecond),
		SessionID:   "sess",
	}
}

func TestTracingGap_FullyCovered(t *testing.T) {
	// Parent 0–100ms, child 0–100ms → gap = 0.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 0, 100),
	}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("fully covered parent should not fire, got %d", len(got))
	}
}

func TestTracingGap_SmallGap_NoFire(t *testing.T) {
	// Parent 0–100ms, child 60–100ms → gap = 60ms but only 40ms uncovered, below 50ms threshold.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 60, 100),
	}
	d := &TracingGapDetector{}
	// uncovered = 60ms; threshold = 50ms → should fire.
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("60ms gap should fire, got %d", len(got))
	}
}

func TestTracingGap_BelowThreshold(t *testing.T) {
	// Parent 0–100ms, child 40–100ms → gap = 40ms < 50ms.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 100),
		gapSpan("child", "root", 40, 100),
	}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("40ms gap should not fire, got %d", len(got))
	}
}

func TestTracingGap_LargeGap(t *testing.T) {
	// Parent 0–500ms, child only covers 100–200ms → gap = 400ms.
	spans := []*storage.Span{
		gapSpan("root", "", 0, 500),
		gapSpan("child", "root", 100, 200),
	}
	d := &TracingGapDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 400ms gap, got %d", len(got))
	}
	if got[0].Kind != "tracing_gap" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	wantGap := int64(400 * time.Millisecond)
	if got[0].WastedNs != wantGap {
		t.Errorf("WastedNs = %d, want %d", got[0].WastedNs, wantGap)
	}
}

func TestTracingGap_NoChildren(t *testing.T) {
	spans := []*storage.Span{gapSpan("root", "", 0, 500)}
	d := &TracingGapDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("leaf span should not fire, got %d", len(got))
	}
}
