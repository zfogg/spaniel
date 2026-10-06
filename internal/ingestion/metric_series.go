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
func (l *metricSeriesLimiter) Admit(stream, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.streams[stream]
	if set == nil {
		set = map[string]struct{}{}
		l.streams[stream] = set
	}
	if _, ok := set[key]; ok {
		return true
	}
	if len(set) >= l.limit {
		return false
	}
	set[key] = struct{}{}
	return true
}
