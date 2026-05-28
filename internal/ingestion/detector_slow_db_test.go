package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func dbSpanWithDur(spanID string, durMs int) *storage.Span {
	return &storage.Span{
		SpanID: spanID, ParentSpanID: "parent",
		Attributes: `{"db.system":"postgresql","db.statement":"SELECT 1"}`,
		StartNs:    0,
		EndNs:      int64(durMs) * int64(time.Millisecond),
		SessionID:  "sess",
	}
}

func TestSlowDB_BelowThreshold(t *testing.T) {
	d := &SlowDBDetector{}
	spans := []*storage.Span{dbSpanWithDur("s1", 200)}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("200ms should not fire, got %d issues", len(got))
	}
}

func TestSlowDB_AtThreshold_NoFire(t *testing.T) {
	d := &SlowDBDetector{}
	spans := []*storage.Span{dbSpanWithDur("s1", 250)}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("exactly 250ms should not fire, got %d issues", len(got))
	}
}

func TestSlowDB_AboveThreshold(t *testing.T) {
	d := &SlowDBDetector{}
	spans := []*storage.Span{dbSpanWithDur("s1", 300)}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 300ms span, got %d", len(got))
	}
	if got[0].Kind != "slow_db" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	wantWasted := int64(50 * time.Millisecond)
	if got[0].WastedNs != wantWasted {
		t.Errorf("WastedNs = %d, want %d", got[0].WastedNs, wantWasted)
	}
}

func TestSlowDB_NonDBSpanIgnored(t *testing.T) {
	d := &SlowDBDetector{}
	spans := []*storage.Span{{
		SpanID: "s1", Attributes: `{"http.url":"https://example.com"}`,
		StartNs: 0, EndNs: int64(500 * time.Millisecond), SessionID: "sess",
	}}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("non-DB span should be ignored, got %d issues", len(got))
	}
}

func TestSlowDB_MultipleSlowSpans(t *testing.T) {
	d := &SlowDBDetector{}
	spans := []*storage.Span{dbSpanWithDur("s1", 400), dbSpanWithDur("s2", 600)}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 2 {
		t.Errorf("expected 2 issues for 2 slow spans, got %d", len(got))
	}
}
