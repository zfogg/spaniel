package ingestion

import "sync"

// metricSeriesLimiter bounds the number of indexed identities for one metric
// stream. Raw OTLP attributes are still persisted; only the query identity is
// collapsed when an untrusted sender would create too many series.
type metricSeriesLimiter struct {
	mu      sync.Mutex
	limit   int
	streams map[string]map[string]struct{}
}

func newMetricSeriesLimiter(limit int) *metricSeriesLimiter {
	return &metricSeriesLimiter{limit: limit, streams: map[string]map[string]struct{}{}}
}

// Seed restores durable admissions at startup. Without this, a restart would
// give an untrusted sender a fresh per-stream budget and defeat the limit.
func (l *metricSeriesLimiter) Seed(stream, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.streams[stream]
	if set == nil {
		set = map[string]struct{}{}
		l.streams[stream] = set
	}
	set[key] = struct{}{}
}

// Admit reports whether the identity can remain indexed and whether it was
// first seen by this process.  The latter is persisted by the pipeline, which
// makes the active-series view survive a Spaniel restart.
func (l *metricSeriesLimiter) Admit(stream, key string) (admitted, fresh bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.streams[stream]
	if set == nil {
		set = map[string]struct{}{}
		l.streams[stream] = set
	}
	if _, ok := set[key]; ok {
		return true, false
	}
	if len(set) >= l.limit {
		return false, false
	}
	set[key] = struct{}{}
	return true, true
}
