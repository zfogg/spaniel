package ingestion

import (
	"testing"
	"time"
)

func fakeNow(sec int64) func() time.Time {
	return func() time.Time { return time.Unix(sec, 0) }
}

func TestThroughput_RollingWindow(t *testing.T) {
	c := newThroughputCounter()

	// Second 1: 3 spans
	c.now = fakeNow(1)
	c.addSpans(3)

	// Second 2: 7 spans
	c.now = fakeNow(2)
	c.addSpans(7)

	// Read at second 2 — window has 10 buckets, only 2 have data
	got := c.Throughput()
	wantSpansPerSec := float64(3+7) / float64(throughputWindow)
	if got.SpansPerSec != wantSpansPerSec {
		t.Errorf("SpansPerSec = %v, want %v", got.SpansPerSec, wantSpansPerSec)
	}
}

func TestThroughput_OldBucketsDecay(t *testing.T) {
	c := newThroughputCounter()

	c.now = fakeNow(1)
	c.addSpans(100)

	// Advance past the window — second 1 should no longer count
	c.now = fakeNow(1 + throughputWindow + 1)
	got := c.Throughput()
	if got.SpansPerSec != 0 {
		t.Errorf("expected 0 after window expires, got %v", got.SpansPerSec)
	}
}

func TestThroughput_PeakTracked(t *testing.T) {
	c := newThroughputCounter()

	c.now = fakeNow(1)
	c.addSpans(50)

	c.now = fakeNow(2)
	c.addSpans(10) // lower than peak

	got := c.Throughput()
	if got.PeakSpansPerSec != 50 {
		t.Errorf("PeakSpansPerSec = %v, want 50", got.PeakSpansPerSec)
	}
}

func TestThroughput_LogsAndMetrics(t *testing.T) {
	c := newThroughputCounter()

	c.now = fakeNow(5)
	c.addLogs(20)
	c.addMetrics(30)

	got := c.Throughput()
	wantLogs := float64(20) / float64(throughputWindow)
	wantMetrics := float64(30) / float64(throughputWindow)
	if got.LogsPerSec != wantLogs {
		t.Errorf("LogsPerSec = %v, want %v", got.LogsPerSec, wantLogs)
	}
	if got.MetricsPerSec != wantMetrics {
		t.Errorf("MetricsPerSec = %v, want %v", got.MetricsPerSec, wantMetrics)
	}
}

func TestThroughput_BucketWraparound(t *testing.T) {
	c := newThroughputCounter()

	// Fill buckets 0..9
	for i := int64(0); i < throughputWindow; i++ {
		c.now = fakeNow(i)
		c.addSpans(1)
	}

	// Advance by one full window — all old buckets should have been evicted
	// as we read at second 10, only seconds 1..10 are within range.
	// Second 0 is outside (10-10=0 is the cutoff, exclusive).
	c.now = fakeNow(throughputWindow)
	c.addSpans(5)

	got := c.Throughput()
	// Seconds 1-9 each have 1, second 10 has 5 → total 14
	want := float64(14) / float64(throughputWindow)
	if got.SpansPerSec != want {
		t.Errorf("SpansPerSec = %v, want %v", got.SpansPerSec, want)
	}
}
