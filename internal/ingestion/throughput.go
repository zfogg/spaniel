package ingestion

import (
	"sync"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// throughputWindow is how many 1-second buckets the rolling counter keeps.
// Rates are reported as the average over this window.
const throughputWindow = 10

// tpBucket holds the counts received during one wall-clock second. sec is the
// unix second it represents, so a stale slot (from >window seconds ago) is
// distinguishable from a current one and gets reset on next use.
type tpBucket struct {
	sec     int64
	spans   int64
	logs    int64
	metrics int64
}

// throughputCounter is a lock-guarded ring of per-second buckets. The clock is
// injectable so tests can feed events across bucket boundaries deterministically.
type throughputCounter struct {
	mu      sync.Mutex
	buckets [throughputWindow]tpBucket
	peak    float64
	now     func() time.Time
}

func newThroughputCounter() *throughputCounter {
	return &throughputCounter{now: time.Now}
}

// bucketFor returns the bucket for the given unix second, resetting it first if
// the slot currently holds a different (older) second. Caller must hold mu.
func (c *throughputCounter) bucketFor(sec int64) *tpBucket {
	b := &c.buckets[sec%throughputWindow]
	if b.sec != sec {
		*b = tpBucket{sec: sec}
	}
	return b
}

func (c *throughputCounter) addSpans(n int) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.bucketFor(c.now().Unix())
	b.spans += int64(n)
	if float64(b.spans) > c.peak {
		c.peak = float64(b.spans)
	}
}

func (c *throughputCounter) addLogs(n int) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bucketFor(c.now().Unix()).logs += int64(n)
}

func (c *throughputCounter) addMetrics(n int) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bucketFor(c.now().Unix()).metrics += int64(n)
}

// Throughput sums the buckets within the trailing window and divides by the
// window length to get a smoothed per-second rate. Buckets older than the
// window are ignored (their sec falls outside the range).
func (c *throughputCounter) Throughput() storage.Throughput {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().Unix()
	cutoff := now - throughputWindow
	var spans, logs, metrics int64
	for i := range c.buckets {
		b := &c.buckets[i]
		if b.sec > cutoff && b.sec <= now {
			spans += b.spans
			logs += b.logs
			metrics += b.metrics
		}
	}
	w := float64(throughputWindow)
	return storage.Throughput{
		SpansPerSec:     float64(spans) / w,
		LogsPerSec:      float64(logs) / w,
		MetricsPerSec:   float64(metrics) / w,
		PeakSpansPerSec: c.peak,
	}
}
