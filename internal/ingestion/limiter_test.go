package ingestion

import (
	"testing"
	"time"
)

func TestSourceLimiter_UnlimitedAllowsAll(t *testing.T) {
	sl := NewSourceLimiter(0, 0)
	for range 1000 {
		if !sl.Allow("svc", false, 100) {
			t.Fatal("unlimited limiter must accept everything")
		}
	}
}

func TestSourceLimiter_BudgetExhausted(t *testing.T) {
	sl := NewSourceLimiter(5, 5)
	accepted := 0
	for range 20 {
		if sl.Allow("svc", false, 100) {
			accepted++
		}
	}
	if accepted != 5 {
		t.Fatalf("expected 5 accepted (burst=5), got %d", accepted)
	}
}

func TestSourceLimiter_PerSourceIsolation(t *testing.T) {
	sl := NewSourceLimiter(5, 5)
	// Exhaust service-A's bucket.
	for range 20 {
		sl.Allow("service-a", false, 100)
	}
	// service-B should still have its full bucket.
	accepted := 0
	for range 5 {
		if sl.Allow("service-b", false, 100) {
			accepted++
		}
	}
	if accepted != 5 {
		t.Fatalf("service-b should be unaffected by service-a flood; got %d/5 accepted", accepted)
	}
}

func TestSourceLimiter_RefillOverTime(t *testing.T) {
	sl := NewSourceLimiter(10, 10)
	// Drain the bucket.
	for range 20 {
		sl.Allow("svc", false, 100)
	}
	// Wait > 1 second for the bucket to refill.
	time.Sleep(1050 * time.Millisecond)
	accepted := 0
	for range 20 {
		if sl.Allow("svc", false, 100) {
			accepted++
		}
	}
	if accepted < 9 || accepted > 11 {
		t.Fatalf("expected ~10 accepted after 1s refill, got %d", accepted)
	}
}

func TestSourceLimiter_SnapshotAcceptedPerSec(t *testing.T) {
	sl := NewSourceLimiter(0, 0)
	for range 60 {
		sl.Allow("svc-a", false, 200)
	}
	for range 30 {
		sl.Allow("svc-b", true, 100)
	}

	snap := map[string]float64{}
	for _, s := range sl.Snapshot() {
		snap[s.Service] = s.AcceptedPerSec
	}
	if snap["svc-a"] <= 0 {
		t.Errorf("svc-a should have positive accepted_per_sec, got %v", snap["svc-a"])
	}
	if snap["svc-b"] <= 0 {
		t.Errorf("svc-b should have positive accepted_per_sec, got %v", snap["svc-b"])
	}
}

func TestSourceLimiter_SnapshotRejectedPerSec(t *testing.T) {
	sl := NewSourceLimiter(1, 1)
	for range 10 {
		sl.Allow("svc", false, 100)
	}

	for _, s := range sl.Snapshot() {
		if s.Service == "svc" && s.RejectedPerSec <= 0 {
			t.Errorf("expected rejected_per_sec > 0 after flood, got %v", s.RejectedPerSec)
		}
	}
}

func TestSourceLimiter_SnapshotErrorRate(t *testing.T) {
	sl := NewSourceLimiter(0, 0)
	// 2 error spans out of 4 total = 0.5 error rate
	sl.Allow("svc", true, 100)
	sl.Allow("svc", true, 100)
	sl.Allow("svc", false, 100)
	sl.Allow("svc", false, 100)

	for _, s := range sl.Snapshot() {
		if s.Service == "svc" {
			if s.ErrorRate < 0.4 || s.ErrorRate > 0.6 {
				t.Errorf("expected error_rate ~0.5, got %v", s.ErrorRate)
			}
			return
		}
	}
	t.Fatal("svc not found in snapshot")
}
