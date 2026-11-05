package ingestion

import (
	"sync"

	"github.com/zfogg/spaniel/internal/storage"
)

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

func (l *metricSeriesLimiter) Forget(stream, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.streams[stream], key)
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

func (p *Pipeline) currentMetricSeriesLimiter() (*metricSeriesLimiter, error) {
	p.metricSeriesMu.Lock()
	defer p.metricSeriesMu.Unlock()
	epoch := p.store.MetricCatalogEpoch()
	if epoch != p.metricSeriesEpoch {
		rows, err := p.store.MetricSeriesCatalog()
		if err != nil {
			return nil, err
		}
		limiter := newMetricSeriesLimiter(storage.MetricSeriesLimit)
		for _, row := range rows {
			limiter.Seed(row.SessionID+"\x00"+row.Service+"\x00"+row.Name, row.Attributes)
		}
		p.metricSeries = limiter
		p.metricSeriesEpoch = epoch
	}
	return p.metricSeries, nil
}
