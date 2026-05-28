package ingestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func httpSpan(id, host string) *storage.Span {
	return &storage.Span{
		SpanID: id, ParentSpanID: "parent",
		Attributes: fmt.Sprintf(`{"server.address":%q}`, host),
		StartNs:    0, EndNs: int64(10 * time.Millisecond),
		SessionID: "sess",
	}
}

func TestChattyHTTP_BelowThreshold(t *testing.T) {
	var spans []*storage.Span
	for i := range 4 {
		spans = append(spans, httpSpan(fmt.Sprintf("s%d", i), "api.example.com"))
	}
	d := &ChattyHTTPDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("4 calls should not fire, got %d", len(got))
	}
}

func TestChattyHTTP_AtThreshold(t *testing.T) {
	var spans []*storage.Span
	for i := range 5 {
		spans = append(spans, httpSpan(fmt.Sprintf("s%d", i), "api.example.com"))
	}
	d := &ChattyHTTPDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 5 calls, got %d", len(got))
	}
	if got[0].Kind != "chatty_http" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	if got[0].Count != 5 {
		t.Errorf("Count = %d, want 5", got[0].Count)
	}
}

func TestChattyHTTP_DifferentHosts(t *testing.T) {
	var spans []*storage.Span
	for i := range 5 {
		spans = append(spans, httpSpan(fmt.Sprintf("s%d", i), fmt.Sprintf("host%d.example.com", i)))
	}
	d := &ChattyHTTPDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("5 distinct hosts should not fire, got %d", len(got))
	}
}

func TestChattyHTTP_URLFullAttribute(t *testing.T) {
	var spans []*storage.Span
	for i := range 5 {
		spans = append(spans, &storage.Span{
			SpanID:     fmt.Sprintf("s%d", i),
			Attributes: `{"url.full":"https://payments.example.com/v1/charge"}`,
			StartNs:    0, EndNs: int64(5 * time.Millisecond), SessionID: "sess",
		})
	}
	d := &ChattyHTTPDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue via url.full, got %d", len(got))
	}
}
