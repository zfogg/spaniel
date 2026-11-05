package ingestion

import (
	"log/slog"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/zfogg/spaniel/internal/goroutine"
	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/storage"
)

const poolMetricWindow = detectors.PoolMetricWindow
const poolMetricFreshness = detectors.PoolMetricFreshness

type poolMetricKey struct {
	session, service, pool, name string
	window                       int64
}
type poolMetricQueue struct {
	mu      sync.Mutex
	pending map[poolMetricKey]struct{}
	running bool
}

// schedulePoolMetric is invoked only after the metric batch has committed. The
// queue coalesces identities and bounds retained work during ingestion bursts.
func (p *Pipeline) schedulePoolMetric(point storage.Metric) {
	var attrs map[string]any
	if json.Unmarshal([]byte(point.Attributes), &attrs) != nil {
		return
	}
	pool, _ := attrs["db.client.connection.pool.name"].(string)
	if pool == "" || point.ServiceName == "" || point.SessionID == "" || point.TimestampNs <= 0 {
		return
	}
	key := poolMetricKey{point.SessionID, point.ServiceName, pool, point.Name, point.TimestampNs / poolMetricWindow * poolMetricWindow}
	q := &p.poolMetrics
	q.mu.Lock()
	if q.pending == nil {
		q.pending = make(map[poolMetricKey]struct{})
	}
	if len(q.pending) >= 256 {
		q.mu.Unlock()
		slog.Warn("pool metric detector queue full")
		return
	}
	q.pending[key] = struct{}{}
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()
	goroutine.Go(func() {
		for {
			q.mu.Lock()
			var key poolMetricKey
			for k := range q.pending {
				key = k
				delete(q.pending, k)
				break
			}
			if key.name == "" {
				q.running = false
				q.mu.Unlock()
				return
			}
			q.mu.Unlock()
			p.evaluatePoolMetric(key, time.Now().UnixNano())
		}
	}, "subsystem", "pool_metric_detector")
}

func (p *Pipeline) evaluatePoolMetric(key poolMetricKey, now int64) {
	if key.window > now || key.window+poolMetricWindow < now-poolMetricFreshness {
		return
	}
	end := min(now, key.window+poolMetricWindow-1)
	points, err := p.store.GetMetricSeries(storage.MetricSeriesFilter{SessionID: key.session, Service: key.service, Name: key.name, Attributes: map[string]string{"db.client.connection.pool.name": key.pool}, FromNs: max(key.window, now-poolMetricFreshness), ToNs: end, Limit: 256})
	if err != nil {
		slog.Warn("pool metric detector read failed", "error", err)
		return
	}
	if detectors.PoolMetricSaturated(key.name, points, now) {
		if err := p.store.RecordPoolMetricFinding(key.session, key.service, key.pool, key.name, key.window); err != nil {
			slog.Warn("pool metric detector finding write failed", "error", err)
		}
	}
}
