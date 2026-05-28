package ingestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func clientSpan(id, parentID string, startMs, endMs int) *storage.Span {
	return &storage.Span{
		SpanID:     id,
		ParentSpanID: parentID,
		Kind:       3, // CLIENT
		Attributes: `{"http.method":"GET"}`,
		StartNs:    int64(startMs) * int64(time.Millisecond),
		EndNs:      int64(endMs) * int64(time.Millisecond),
		SessionID:  "sess",
	}
}

func TestSerialPromise_SerialCallsFire(t *testing.T) {
	// Parent 0–400ms. Three sequential 100ms client calls accounting for 300ms (75% < 80% threshold?)
	// Let's use 4 calls to ensure we hit 80%.
	parent := &storage.Span{
		SpanID: "parent", Kind: 2,
		StartNs: 0, EndNs: int64(400 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{parent}
	for i := range 4 {
		spans = append(spans, clientSpan(fmt.Sprintf("c%d", i), "parent", i*90, i*90+80))
	}
	d := &SerialPromiseDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for serial calls, got %d", len(got))
	}
	if got[0].Kind != "serial_promise" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
}

func TestSerialPromise_TooFewChildren(t *testing.T) {
	parent := &storage.Span{
		SpanID: "parent", Kind: 2,
		StartNs: 0, EndNs: int64(300 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{parent,
		clientSpan("c0", "parent", 0, 100),
		clientSpan("c1", "parent", 100, 200),
	}
	d := &SerialPromiseDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("2 children below min, got %d issues", len(got))
	}
}

func TestSerialPromise_OverlappingChildren_NoFire(t *testing.T) {
	// Children overlap — not serial.
	parent := &storage.Span{
		SpanID: "parent", Kind: 2,
		StartNs: 0, EndNs: int64(300 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{parent,
		clientSpan("c0", "parent", 0, 200),
		clientSpan("c1", "parent", 50, 250),  // overlaps c0
		clientSpan("c2", "parent", 100, 300), // overlaps c1
	}
	d := &SerialPromiseDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("overlapping children should not fire, got %d issues", len(got))
	}
}
