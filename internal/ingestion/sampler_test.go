package ingestion

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestSampler_UnlimitedBudget(t *testing.T) {
	s := NewSampler(0, KeepErrors)
	for range 1000 {
		if !s.DecideSpan(&storage.Span{StatusCode: 0}) {
			t.Fatal("budget=0 should accept everything")
		}
	}
}

func TestSampler_TokenBucketRefill(t *testing.T) {
	// budget=10 fills at 10/sec; start full, drain, then wait for refill
	s := NewSampler(10, 0)

	// Drain all tokens immediately
	accepted := 0
	for range 20 {
		if s.DecideSpan(&storage.Span{}) {
			accepted++
		}
	}
	if accepted != 10 {
		t.Fatalf("expected 10 tokens available at start, got %d", accepted)
	}

	// Wait > 1 second so the bucket refills
	time.Sleep(1050 * time.Millisecond)

	// Should have ~10 new tokens
	accepted2 := 0
	for range 20 {
		if s.DecideSpan(&storage.Span{}) {
			accepted2++
		}
	}
	if accepted2 < 9 || accepted2 > 11 {
		t.Fatalf("expected ~10 tokens after 1s refill, got %d", accepted2)
	}
}

func TestSampler_BurstCappedAtBudget(t *testing.T) {
	s := NewSampler(5, 0)
	// Even after a long sleep the bucket is capped at budget
	time.Sleep(50 * time.Millisecond)
	accepted := 0
	for range 20 {
		if s.DecideSpan(&storage.Span{}) {
			accepted++
		}
	}
	if accepted > 5 {
		t.Fatalf("burst should be capped at budget=5, got %d", accepted)
	}
}

func TestSampler_ErrorsAlwaysKept(t *testing.T) {
	// budget=1 will be exhausted immediately; error spans still pass
	s := NewSampler(1, KeepErrors)

	// Drain the bucket
	s.DecideSpan(&storage.Span{StatusCode: 0})

	// Error span must still be accepted even with empty bucket
	if !s.DecideSpan(&storage.Span{StatusCode: 2}) {
		t.Fatal("error span (status_code=2) must be accepted regardless of budget")
	}
}

func TestSampler_NoPolicyKeepErrors(t *testing.T) {
	// With KeepErrors=0 error spans are rate-limited too
	s := NewSampler(1, 0)
	s.DecideSpan(&storage.Span{StatusCode: 0}) // drain

	// Error span should be dropped because KeepErrors is not in alwaysKeep
	if s.DecideSpan(&storage.Span{StatusCode: 2}) {
		t.Fatal("error span should be rate-limited when KeepErrors flag is not set")
	}
}

func TestSampler_DropCountersIncrement(t *testing.T) {
	s := NewSampler(1, 0)
	s.DecideSpan(&storage.Span{}) // consume token

	// These should be dropped and counted
	for range 5 {
		if s.DecideSpan(&storage.Span{}) {
			t.Fatal("should be dropping with empty bucket")
		}
		s.Counters.bumpSpans(1)
	}
	if s.Counters.DroppedSpansTotal() != 5 {
		t.Fatalf("expected 5 dropped spans, got %d", s.Counters.DroppedSpansTotal())
	}
	if s.Counters.LastDropAt() == 0 {
		t.Fatal("LastDropAt should be set after drops")
	}
}

func TestParseAlwaysKeep(t *testing.T) {
	tests := []struct {
		in   string
		want AlwaysKeep
	}{
		{"error,n_plus_one,slow", KeepErrors | KeepNPlusOne | KeepSlow},
		{"error", KeepErrors},
		{"none", 0},
		{"error,none", 0},
		{"slow,error", KeepSlow | KeepErrors},
		{"", 0},
	}
	for _, tc := range tests {
		if got := ParseAlwaysKeep(tc.in); got != tc.want {
			t.Errorf("ParseAlwaysKeep(%q) = %b, want %b", tc.in, got, tc.want)
		}
	}
}
