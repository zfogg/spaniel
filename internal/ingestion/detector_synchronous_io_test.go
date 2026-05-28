package ingestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func serverGetSpan(id string) *storage.Span {
	return &storage.Span{
		SpanID:     id,
		Kind:       2, // SERVER
		Attributes: `{"http.method":"GET","http.route":"/api/items"}`,
		StartNs:    0, EndNs: int64(300 * time.Millisecond),
		SessionID: "sess",
	}
}

func inlineDBSpan(id, parentID string) *storage.Span {
	return &storage.Span{
		SpanID:     id,
		ParentSpanID: parentID,
		Attributes: `{"db.system":"postgresql","db.statement":"SELECT 1"}`,
		StartNs:    0, EndNs: int64(20 * time.Millisecond),
		SessionID: "sess",
	}
}

func TestSynchronousIO_ThreeDBChildrenFire(t *testing.T) {
	root := serverGetSpan("root")
	spans := []*storage.Span{root}
	for i := range 3 {
		spans = append(spans, inlineDBSpan(fmt.Sprintf("db%d", i), "root"))
	}
	d := &SynchronousIODetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for GET + 3 DB children, got %d", len(got))
	}
	if got[0].Kind != "synchronous_io" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	if got[0].Count != 3 {
		t.Errorf("Count = %d, want 3", got[0].Count)
	}
}

func TestSynchronousIO_TwoDBChildren_NoFire(t *testing.T) {
	root := serverGetSpan("root")
	spans := []*storage.Span{root,
		inlineDBSpan("db0", "root"),
		inlineDBSpan("db1", "root"),
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("2 DB children should not fire, got %d", len(got))
	}
}

func TestSynchronousIO_PostMethod_NoFire(t *testing.T) {
	root := &storage.Span{
		SpanID: "root", Kind: 2,
		Attributes: `{"http.method":"POST"}`,
		StartNs: 0, EndNs: int64(200 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{root}
	for i := range 3 {
		spans = append(spans, inlineDBSpan(fmt.Sprintf("db%d", i), "root"))
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("POST method should not fire, got %d", len(got))
	}
}

func TestSynchronousIO_ClientSpan_NoFire(t *testing.T) {
	// Kind 3 = CLIENT — not a server span.
	root := &storage.Span{
		SpanID: "root", Kind: 3,
		Attributes: `{"http.method":"GET"}`,
		StartNs: 0, EndNs: int64(200 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{root}
	for i := range 3 {
		spans = append(spans, inlineDBSpan(fmt.Sprintf("db%d", i), "root"))
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("client span should not fire, got %d", len(got))
	}
}
