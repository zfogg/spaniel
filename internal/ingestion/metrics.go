package ingestion

import (
	"encoding/json"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// ingestMetrics walks an OTLP Metrics tree and emits storage.Metric rows.
// Gauges and Sums become one row per data point. Histograms become 3 rows
// per data point (p50/p95/p99) with the percentile encoded in attributes.
func (p *Pipeline) ingestMetricsTree(md pmetric.Metrics, sessionID string) error {
	receivedAt := time.Now().UnixNano()

	pointsSeen := 0
	defer func() { p.tp.addMetrics(pointsSeen) }()

	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		svc := serviceNameFromAttrs(rm.Resource().Attributes())

		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				pts := metricDataPointCount(m)
				if !p.sampler.DecideMetric() {
					p.sampler.Counters.bumpMetrics(int64(pts))
					continue
				}
				pointsSeen += pts
				if err := p.storeMetric(m, svc, sessionID, receivedAt); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// metricDataPointCount reports how many OTLP data points a metric carries,
// counting one per histogram point (not the p50/p95/p99 rows we expand it into).
func metricDataPointCount(m pmetric.Metric) int {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		return m.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		return m.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		return m.Histogram().DataPoints().Len()
	default:
		return 0
	}
}

func (p *Pipeline) storeMetric(m pmetric.Metric, svc, sessionID string, _ int64) error {
	name, desc, unit := m.Name(), m.Description(), m.Unit()

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		for i := 0; i < m.Gauge().DataPoints().Len(); i++ {
			dp := m.Gauge().DataPoints().At(i)
			row := &storage.Metric{
				Name:        name,
				Description: desc,
				Unit:        unit,
				Type:        "gauge",
				TimestampNs: int64(dp.Timestamp()),
				Value:       numericValue(dp),
				Attributes:  mapToJSON(dp.Attributes()),
				ServiceName: svc,
				SessionID:   sessionID,
			}
			if err := p.store.InsertMetric(row); err != nil {
				return err
			}
			p.hub.Broadcast(ws.NewMetricEvent(&ws.MetricPayload{
				Name:        row.Name,
				ServiceName: row.ServiceName,
				Value:       row.Value,
				Type:        row.Type,
			}))
		}

	case pmetric.MetricTypeSum:
		for i := 0; i < m.Sum().DataPoints().Len(); i++ {
			dp := m.Sum().DataPoints().At(i)
			row := &storage.Metric{
				Name:        name,
				Description: desc,
				Unit:        unit,
				Type:        "counter",
				TimestampNs: int64(dp.Timestamp()),
				Value:       numericValue(dp),
				Attributes:  mapToJSON(dp.Attributes()),
				ServiceName: svc,
				SessionID:   sessionID,
			}
			if err := p.store.InsertMetric(row); err != nil {
				return err
			}
			p.hub.Broadcast(ws.NewMetricEvent(&ws.MetricPayload{
				Name:        row.Name,
				ServiceName: row.ServiceName,
				Value:       row.Value,
				Type:        row.Type,
			}))
		}

	case pmetric.MetricTypeHistogram:
		for i := 0; i < m.Histogram().DataPoints().Len(); i++ {
			dp := m.Histogram().DataPoints().At(i)
			bounds := boundsToFloat(dp.ExplicitBounds())
			counts := countsToInt(dp.BucketCounts())
			for _, pct := range []float64{0.50, 0.95, 0.99} {
				var v float64
				if len(bounds) == 0 {
					// No explicit bucket boundaries — use sum/count mean as best estimate.
					if dp.Count() > 0 {
						v = dp.Sum() / float64(dp.Count())
					}
				} else {
					v = HistogramPercentile(bounds, counts, pct)
				}
				attrs := attrsWithPercentile(dp.Attributes(), pct)
				row := &storage.Metric{
					Name:        name,
					Description: desc,
					Unit:        unit,
					Type:        "histogram",
					TimestampNs: int64(dp.Timestamp()),
					Value:       v,
					Attributes:  attrs,
					ServiceName: svc,
					SessionID:   sessionID,
				}
				if err := p.store.InsertMetric(row); err != nil {
					return err
				}
				p.hub.Broadcast(ws.NewMetricEvent(&ws.MetricPayload{
					Name:        row.Name,
					ServiceName: row.ServiceName,
					Value:       row.Value,
					Type:        row.Type,
				}))
			}
		}

	default:
		// Summary / ExponentialHistogram unsupported in v1 — skip silently.
	}
	return nil
}

func numericValue(dp pmetric.NumberDataPoint) float64 {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		return dp.DoubleValue()
	case pmetric.NumberDataPointValueTypeInt:
		return float64(dp.IntValue())
	default:
		return 0
	}
}

func boundsToFloat(s pcommon.Float64Slice) []float64 {
	out := make([]float64, s.Len())
	for i := 0; i < s.Len(); i++ {
		out[i] = s.At(i)
	}
	return out
}

func countsToInt(s pcommon.UInt64Slice) []uint64 {
	out := make([]uint64, s.Len())
	for i := 0; i < s.Len(); i++ {
		out[i] = s.At(i)
	}
	return out
}

func attrsWithPercentile(attrs pcommon.Map, p float64) string {
	raw := make(map[string]any, attrs.Len()+1)
	attrs.Range(func(k string, v pcommon.Value) bool {
		raw[k] = v.AsRaw()
		return true
	})
	switch p {
	case 0.50:
		raw["percentile"] = "p50"
	case 0.95:
		raw["percentile"] = "p95"
	case 0.99:
		raw["percentile"] = "p99"
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

// HistogramPercentile approximates the given percentile from explicit-bucket
// histogram data. bounds is an ascending list of upper bounds (length N);
// counts is the per-bucket counts (length N+1, the last bucket has no upper
// bound). Uses linear interpolation within the bucket that contains the
// percentile. Returns 0 if the histogram has no observations.
//
// This is the exported entry point so the algorithm can be unit-tested
// without pmetric machinery.
func HistogramPercentile(bounds []float64, counts []uint64, p float64) float64 {
	var total uint64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	target := float64(total) * p

	var cum float64
	for i, c := range counts {
		next := cum + float64(c)
		if next >= target {
			// We are inside bucket i. Compute its [lo, hi].
			var lo, hi float64
			switch {
			case i == 0 && len(bounds) > 0:
				lo, hi = 0, bounds[0]
			case i < len(bounds):
				lo, hi = bounds[i-1], bounds[i]
			default:
				// Final overflow bucket — return its lower bound (best we can do).
				if len(bounds) > 0 {
					return bounds[len(bounds)-1]
				}
				return 0
			}
			if c == 0 {
				return hi
			}
			frac := (target - cum) / float64(c)
			return lo + frac*(hi-lo)
		}
		cum = next
	}
	if len(bounds) > 0 {
		return bounds[len(bounds)-1]
	}
	return 0
}
