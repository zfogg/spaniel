package ingestion

import (
	"sync"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

const sourceWindowSec = 60

// sourceBucket is one second's worth of raw counters for a single source.
type sourceBucket struct {
	accepted int64
	rejected int64
	errors   int64
	bytes    int64
}

// sourceEntry holds the token bucket and rolling window for one source.
type sourceEntry struct {
	mu      sync.Mutex
	tokens  float64
	lastAt  time.Time

	// ring of sourceWindowSec buckets indexed by unix second % window
	ring    [sourceWindowSec]sourceBucket
	lastSec int64 // unix second when ring was last advanced

	lastSeenNs int64
}

func (e *sourceEntry) refill(rps float64, burst int) {
	now := time.Now()
	elapsed := now.Sub(e.lastAt).Seconds()
	e.lastAt = now
	e.tokens += elapsed * rps
	if max := float64(burst); e.tokens > max {
		e.tokens = max
	}
}

// allow tries to consume one token from the bucket; returns false if exhausted.
func (e *sourceEntry) allow(rps float64, burst int) bool {
	e.refill(rps, burst)
	if e.tokens < 1 {
		return false
	}
	e.tokens--
	return true
}

// record advances the ring to the current second and increments counters.
func (e *sourceEntry) record(accepted, rejected, isError bool, bytes int64) {
	sec := time.Now().Unix()
	idx := int(sec % sourceWindowSec)
	if sec != e.lastSec {
		// Clear any stale buckets between lastSec and now.
		for s := e.lastSec + 1; s <= sec; s++ {
			e.ring[int(s%sourceWindowSec)] = sourceBucket{}
		}
		e.lastSec = sec
	}
	b := &e.ring[idx]
	if accepted {
		b.accepted++
		b.bytes += bytes
		if isError {
			b.errors++
		}
	}
	if rejected {
		b.rejected++
	}
	e.lastSeenNs = time.Now().UnixNano()
}

// snapshot computes per-second averages over the last sourceWindowSec seconds.
func (e *sourceEntry) snapshot(service string) storage.SourceStats {
	now := time.Now().Unix()
	var totAcc, totRej, totErr, totBytes int64
	for s := now - sourceWindowSec + 1; s <= now; s++ {
		b := e.ring[int(s%sourceWindowSec)]
		// only count buckets that belong to this window (not stale overwrites)
		if s > e.lastSec {
			continue
		}
		if now-s >= sourceWindowSec {
			continue
		}
		totAcc += b.accepted
		totRej += b.rejected
		totErr += b.errors
		totBytes += b.bytes
	}
	var errRate float64
	if totAcc > 0 {
		errRate = float64(totErr) / float64(totAcc)
	}
	return storage.SourceStats{
		Service:        service,
		AcceptedPerSec: float64(totAcc) / float64(sourceWindowSec),
		RejectedPerSec: float64(totRej) / float64(sourceWindowSec),
		ErrorRate:      errRate,
		BytesPerSec:    float64(totBytes) / float64(sourceWindowSec),
		LastSeenNs:     e.lastSeenNs,
	}
}

// SourceLimiter manages per-source token buckets and rolling stats.
// RPS==0 disables rate limiting (all spans accepted) but still tracks stats.
type SourceLimiter struct {
	RPS   float64 // fill rate per source; 0 = unlimited
	Burst int     // bucket capacity; defaults to RPS*5 when 0

	mu      sync.RWMutex
	sources map[string]*sourceEntry
}

// NewSourceLimiter creates a limiter. rps==0 means track-only (no limiting).
func NewSourceLimiter(rps float64, burst int) *SourceLimiter {
	if burst <= 0 && rps > 0 {
		burst = max(1, int(rps*5))
	}
	return &SourceLimiter{
		RPS:     rps,
		Burst:   burst,
		sources: make(map[string]*sourceEntry),
	}
}

// Allow checks whether a span from the given service should be accepted.
// It always records the outcome in the rolling window.
// bytes is an estimate of the span payload size (used for bytes/s stats).
func (sl *SourceLimiter) Allow(service string, isError bool, bytes int64) bool {
	sl.mu.Lock()
	e, ok := sl.sources[service]
	if !ok {
		e = &sourceEntry{lastAt: time.Now(), lastSec: time.Now().Unix()}
		if sl.RPS > 0 {
			e.tokens = float64(sl.Burst)
		}
		sl.sources[service] = e
	}
	sl.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	accepted := sl.RPS == 0 || e.allow(sl.RPS, sl.Burst)
	e.record(accepted, !accepted, isError, bytes)
	return accepted
}

// Snapshot returns a per-second stats row for every source seen so far.
func (sl *SourceLimiter) Snapshot() []storage.SourceStats {
	sl.mu.RLock()
	keys := make([]string, 0, len(sl.sources))
	for k := range sl.sources {
		keys = append(keys, k)
	}
	sl.mu.RUnlock()

	out := make([]storage.SourceStats, 0, len(keys))
	for _, svc := range keys {
		sl.mu.RLock()
		e := sl.sources[svc]
		sl.mu.RUnlock()

		e.mu.Lock()
		s := e.snapshot(svc)
		e.mu.Unlock()
		out = append(out, s)
	}
	return out
}

