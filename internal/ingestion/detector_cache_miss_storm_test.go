package ingestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func cacheMissSpan(id string, offsetMs int) *storage.Span {
	start := int64(offsetMs) * int64(time.Millisecond)
	return &storage.Span{
		SpanID:     id,
		Attributes: `{"db.system":"redis","cache.hit":false}`,
		StartNs:    start,
		EndNs:      start + int64(time.Millisecond),
		SessionID:  "sess",
	}
}

func TestCacheMissStorm_BelowThreshold(t *testing.T) {
	var spans []*storage.Span
	for i := range 9 {
		spans = append(spans, cacheMissSpan(fmt.Sprintf("s%d", i), i))
	}
	d := &CacheMissStormDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("9 misses should not fire, got %d", len(got))
	}
}

func TestCacheMissStorm_TenWithinWindow(t *testing.T) {
	var spans []*storage.Span
	// 10 misses all within 30ms
	for i := range 10 {
		spans = append(spans, cacheMissSpan(fmt.Sprintf("s%d", i), i*3))
	}
	d := &CacheMissStormDetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for 10 misses in 30ms, got %d", len(got))
	}
	if got[0].Kind != "cache_miss_storm" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
}

func TestCacheMissStorm_SpreadBeyondWindow(t *testing.T) {
	var spans []*storage.Span
	// 10 misses spread over 200ms — no 50ms window contains >= 10.
	for i := range 10 {
		spans = append(spans, cacheMissSpan(fmt.Sprintf("s%d", i), i*20))
	}
	d := &CacheMissStormDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("misses spread >50ms should not fire, got %d", len(got))
	}
}

func TestCacheMissStorm_NonCacheSpansIgnored(t *testing.T) {
	var spans []*storage.Span
	for i := range 10 {
		spans = append(spans, &storage.Span{
			SpanID:     fmt.Sprintf("s%d", i),
			Attributes: `{"http.url":"https://api.example.com"}`,
			StartNs:    int64(i) * int64(time.Millisecond),
			EndNs:      int64(i+1) * int64(time.Millisecond),
			SessionID:  "sess",
		})
	}
	d := &CacheMissStormDetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("non-cache spans should be ignored, got %d", len(got))
	}
}
