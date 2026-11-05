package detectors

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestPoolMetricEvidence(t *testing.T) {
	if got := pmetric.AggregationTemporalityCumulative.String(); got != "Cumulative" {
		t.Fatalf("unexpected OTLP temporality: %q", got)
	}
	now := int64(20 * time.Minute)
	gauge := func(age time.Duration, value float64) *storage.Metric {
		return &storage.Metric{Type: "gauge", TimestampNs: now - int64(age), Value: value, Attributes: `{}`, Resource: `{}`}
	}
	pending := "db.client.connection.pending_requests"
	if PoolMetricSaturated(pending, []*storage.Metric{gauge(time.Minute, 0), gauge(30*time.Second, 0)}, now) {
		t.Fatal("healthy load")
	}
	if PoolMetricSaturated(pending, []*storage.Metric{gauge(3*time.Minute, 1), gauge(30*time.Second, 1)}, now) {
		t.Fatal("stale evidence")
	}
	if PoolMetricSaturated(pending, nil, now) {
		t.Fatal("missing instrumentation")
	}
	if !PoolMetricSaturated(pending, []*storage.Metric{gauge(time.Minute, 1), gauge(30*time.Second, 2)}, now) {
		t.Fatal("sustained pending requests")
	}
	if PoolMetricSaturated(pending, []*storage.Metric{gauge(time.Minute, 1), gauge(50*time.Second, 0), gauge(30*time.Second, 1)}, now) {
		t.Fatal("interrupted pending requests")
	}
	monotonic := true
	counter := func(age time.Duration, start int64, value float64) *storage.Metric {
		return &storage.Metric{Type: "sum", IsMonotonic: &monotonic, AggregationTemporality: "Cumulative", StartTimestampNs: start, TimestampNs: now - int64(age), Value: value, Attributes: `{}`, Resource: `{}`}
	}
	timeouts := "db.client.connection.timeouts"
	if PoolMetricSaturated(timeouts, []*storage.Metric{counter(time.Minute, 1, 8), counter(30*time.Second, 2, 1)}, now) {
		t.Fatal("counter reset")
	}
	if PoolMetricSaturated(timeouts, []*storage.Metric{counter(time.Minute, 1, 8), counter(30*time.Second, 1, 1)}, now) {
		t.Fatal("counter decrease")
	}
	if !PoolMetricSaturated(timeouts, []*storage.Metric{counter(time.Minute, 1, 8), counter(30*time.Second, 1, 9)}, now) {
		t.Fatal("timeout increment")
	}
	if PoolMetricSaturated(timeouts, []*storage.Metric{counter(time.Minute, 1, 8), counter(time.Minute, 1, 9)}, now) {
		t.Fatal("equal timestamps")
	}
}
