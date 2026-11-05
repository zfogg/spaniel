package detectors

import (
	"math"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// PoolMetricWindow and PoolMetricFreshness bound the evaluation interval.
const PoolMetricWindow = int64(5 * time.Minute)
const PoolMetricFreshness = int64(2 * time.Minute)

// PoolMetricSaturated evaluates timestamp-ordered points from one pool and
// window. A gauge needs two positive observations at least ten seconds apart.
// A counter needs a positive delta with matching stream and start time.
func PoolMetricSaturated(name string, points []*storage.Metric, now int64) bool {
	previous := map[string]*storage.Metric{}
	for _, p := range points {
		if p.TimestampNs > now || p.TimestampNs < now-PoolMetricFreshness || p.Flags&1 != 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) || p.Value < 0 {
			continue
		}
		stream := p.Attributes + "\x00" + p.Resource + "\x00" + p.ScopeName + "\x00" + p.ScopeVersion
		prev := previous[stream]
		previous[stream] = p
		if name == "db.client.connection.pending_requests" && p.Type == "gauge" && prev != nil && prev.Type == "gauge" && prev.Value > 0 && p.Value > 0 && p.TimestampNs-prev.TimestampNs >= int64(10*time.Second) {
			return true
		}
		if name == "db.client.connection.timeouts" && p.Type == "sum" && p.IsMonotonic != nil && *p.IsMonotonic && prev != nil && prev.Type == "sum" && prev.IsMonotonic != nil && *prev.IsMonotonic && p.AggregationTemporality == "Cumulative" && prev.AggregationTemporality == "Cumulative" && p.StartTimestampNs > 0 && p.StartTimestampNs == prev.StartTimestampNs && p.TimestampNs > prev.TimestampNs && p.Value > prev.Value {
			return true
		}
	}
	return false
}
