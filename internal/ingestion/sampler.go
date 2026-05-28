package ingestion

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// AlwaysKeep is a bitfield of the signal categories that bypass the token bucket.
type AlwaysKeep uint8

const (
	KeepErrors   AlwaysKeep = 1 << iota // status_code = 2
	KeepNPlusOne                        // trace has an n_plus_one issue
	KeepSlow                            // duration > 2× per-service p95
)

// ParseAlwaysKeep parses a comma-separated list of "error", "n_plus_one", "slow", "none".
func ParseAlwaysKeep(s string) AlwaysKeep {
	var out AlwaysKeep
	for tok := range strings.SplitSeq(s, ",") {
		switch strings.TrimSpace(tok) {
		case "error":
			out |= KeepErrors
		case "n_plus_one":
			out |= KeepNPlusOne
		case "slow":
			out |= KeepSlow
		case "none":
			return 0
		}
	}
	return out
}

// Counters holds process-level drop counters. All fields are atomically updated.
// It implements api.DropCounterProvider via the four exported methods below.
type Counters struct {
	droppedSpans        atomic.Int64
	droppedLogs         atomic.Int64
	droppedMetricPoints atomic.Int64
	lastDropAt          atomic.Int64 // unix nano
}

func (c *Counters) bumpSpans(n int64) {
	c.droppedSpans.Add(n)
	c.lastDropAt.Store(time.Now().UnixNano())
}

func (c *Counters) bumpLogs(n int64) {
	c.droppedLogs.Add(n)
	c.lastDropAt.Store(time.Now().UnixNano())
}

func (c *Counters) bumpMetrics(n int64) {
	c.droppedMetricPoints.Add(n)
	c.lastDropAt.Store(time.Now().UnixNano())
}

// DroppedSpansTotal implements api.DropCounterProvider.
func (c *Counters) DroppedSpansTotal() int64 { return c.droppedSpans.Load() }

// DroppedLogsTotal implements api.DropCounterProvider.
func (c *Counters) DroppedLogsTotal() int64 { return c.droppedLogs.Load() }

// DroppedMetricPointsTotal implements api.DropCounterProvider.
func (c *Counters) DroppedMetricPointsTotal() int64 { return c.droppedMetricPoints.Load() }

// LastDropAt implements api.DropCounterProvider.
func (c *Counters) LastDropAt() int64 { return c.lastDropAt.Load() }

// Sampler applies tail-based sampling to incoming telemetry.
// When budget == 0 every span/log/metric is accepted (default off behaviour).
type Sampler struct {
	budget     int // spans per second; 0 = unlimited
	alwaysKeep AlwaysKeep
	Counters   Counters

	mu     sync.Mutex
	tokens float64
	lastAt time.Time
}

// NewSampler creates a Sampler. budget=0 disables rate limiting.
func NewSampler(budget int, keep AlwaysKeep) *Sampler {
	s := &Sampler{
		budget:     budget,
		alwaysKeep: keep,
	}
	if budget > 0 {
		s.tokens = float64(budget)
		s.lastAt = time.Now()
	}
	return s
}

// DecideSpan returns true if the span should be stored, false if it should be
// dropped. Error spans and other always-keep signals bypass the budget.
func (s *Sampler) DecideSpan(span *storage.Span) bool {
	if s.budget == 0 {
		return true
	}
	if s.alwaysKeep&KeepErrors != 0 && span.StatusCode == 2 {
		return true
	}
	return s.consume()
}

// DecideLog always returns true when budget==0; otherwise consumes a token.
func (s *Sampler) DecideLog() bool {
	if s.budget == 0 {
		return true
	}
	return s.consume()
}

// DecideMetric always returns true when budget==0; otherwise consumes a token.
func (s *Sampler) DecideMetric() bool {
	if s.budget == 0 {
		return true
	}
	return s.consume()
}

// consume tries to take one token, refilling the bucket from elapsed time first.
func (s *Sampler) consume() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.lastAt).Seconds()
	s.lastAt = now

	s.tokens += elapsed * float64(s.budget)
	if max := float64(s.budget); s.tokens > max {
		s.tokens = max
	}

	if s.tokens < 1 {
		return false
	}
	s.tokens--
	return true
}
