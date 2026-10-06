package ingestion

import (
	"context"
	"fmt"

	json "github.com/goccy/go-json"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

// The indexed metric identity is intentionally much smaller than the OTLP
// attribute set. OTLP clients are untrusted and routinely attach IDs, URLs and
// raw errors; those remain on the point but cannot create a query series.
var indexedMetricAttributes = map[string]struct{}{
	"http.request.method": {}, "http.response.status_code": {}, "http.route": {},
	"rpc.method": {}, "rpc.service": {}, "db.system": {}, "messaging.system": {},
	"signal": {}, "result": {}, "reason": {}, "operation": {},
}

// ingestMetrics walks the OTLP tree without changing the metric model. Every
// OTLP data point becomes exactly one storage row.
func (p *Pipeline) ingestMetricsTree(ctx context.Context, md pmetric.Metrics, sessionID string) error {
	pointsSeen := 0
	defer func() { p.tp.addMetrics(pointsSeen) }()
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		svc, resource := serviceNameFromAttrs(rm.Resource().Attributes()), mapToJSON(rm.Resource().Attributes())
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			scope := sm.Scope()
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				pts := metricDataPointCount(m)
				if !p.sampler.DecideMetric() {
					p.sampler.Counters.bumpMetrics(int64(pts))
					continue
				}
				pointsSeen += pts
				if err := p.storeMetric(ctx, m, svc, resource, scope.Name(), scope.Version(), sm.SchemaUrl(), mapToJSON(scope.Attributes()), sessionID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func metricDataPointCount(m pmetric.Metric) int {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		return m.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		return m.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		return m.Histogram().DataPoints().Len()
	case pmetric.MetricTypeExponentialHistogram:
		return m.ExponentialHistogram().DataPoints().Len()
	case pmetric.MetricTypeSummary:
		return m.Summary().DataPoints().Len()
	}
	return 0
}

func (p *Pipeline) storeMetric(ctx context.Context, m pmetric.Metric, svc, resource, scopeName, scopeVersion, scopeSchemaURL, scopeAttributes, sessionID string) error {
	base := func(metricType string, attrs pcommon.Map, start, timestamp pcommon.Timestamp, flags pmetric.DataPointFlags, exemplars pmetric.ExemplarSlice) *storage.Metric {
		attrsJSON, seriesAttrs, limitedAttrs := metricAttributes(attrs)
		stream := sessionID + "\x00" + svc + "\x00" + m.Name()
		if !p.metricSeries.Admit(stream, seriesAttrs) {
			seriesAttrs = "{}"
			telemetry.Catalog().RecordCardinalityLimited(ctx, "new_series_budget", 1)
		}
		if limitedAttrs > 0 {
			telemetry.Catalog().RecordCardinalityLimited(ctx, "attribute_not_indexed", int64(limitedAttrs))
		}
		return &storage.Metric{Name: m.Name(), Description: m.Description(), Unit: m.Unit(), Type: metricType,
			StartTimestampNs: int64(start), TimestampNs: int64(timestamp), Flags: uint32(flags),
			Attributes: attrsJSON, Resource: resource, SeriesAttributes: seriesAttrs,
			SeriesKey: m.Name() + "\x00" + svc + "\x00" + seriesAttrs, ScopeName: scopeName, ScopeVersion: scopeVersion, ScopeSchemaURL: scopeSchemaURL, ScopeAttributes: scopeAttributes,
			Exemplars: exemplarsToJSON(exemplars), ServiceName: svc, SessionID: sessionID}
	}
	store := func(row *storage.Metric) error {
		if err := p.store.AppendMetric(row); err != nil {
			return err
		}
		value := row.Value
		if row.HistogramSum != nil {
			value = *row.HistogramSum
		} else if row.SummarySum != nil {
			value = *row.SummarySum
		}
		p.hub.Broadcast(ws.NewMetricEvent(&ws.MetricPayload{Name: row.Name, ServiceName: row.ServiceName, Value: value, Type: row.Type}))
		return nil
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		for i := 0; i < m.Gauge().DataPoints().Len(); i++ {
			dp := m.Gauge().DataPoints().At(i)
			row := base("gauge", dp.Attributes(), dp.StartTimestamp(), dp.Timestamp(), dp.Flags(), dp.Exemplars())
			v := numericValue(dp)
			row.Value = v
			if err := store(row); err != nil {
				return err
			}
		}
	case pmetric.MetricTypeSum:
		monotonic, temporality := m.Sum().IsMonotonic(), m.Sum().AggregationTemporality().String()
		for i := 0; i < m.Sum().DataPoints().Len(); i++ {
			dp := m.Sum().DataPoints().At(i)
			row := base("sum", dp.Attributes(), dp.StartTimestamp(), dp.Timestamp(), dp.Flags(), dp.Exemplars())
			v := numericValue(dp)
			row.Value, row.IsMonotonic, row.AggregationTemporality = v, &monotonic, temporality
			if err := store(row); err != nil {
				return err
			}
		}
	case pmetric.MetricTypeHistogram:
		temporality := m.Histogram().AggregationTemporality().String()
		for i := 0; i < m.Histogram().DataPoints().Len(); i++ {
			dp := m.Histogram().DataPoints().At(i)
			row := base("histogram", dp.Attributes(), dp.StartTimestamp(), dp.Timestamp(), dp.Flags(), dp.Exemplars())
			count := dp.Count()
			row.HistogramCount, row.AggregationTemporality = &count, temporality
			if dp.HasSum() {
				v := dp.Sum()
				row.HistogramSum = &v
			}
			if dp.HasMin() {
				v := dp.Min()
				row.HistogramMin = &v
			}
			if dp.HasMax() {
				v := dp.Max()
				row.HistogramMax = &v
			}
			row.ExplicitBounds, row.BucketCounts = floatSliceJSON(dp.ExplicitBounds()), uint64SliceJSON(dp.BucketCounts())
			if err := store(row); err != nil {
				return err
			}
		}
	case pmetric.MetricTypeExponentialHistogram:
		temporality := m.ExponentialHistogram().AggregationTemporality().String()
		for i := 0; i < m.ExponentialHistogram().DataPoints().Len(); i++ {
			dp := m.ExponentialHistogram().DataPoints().At(i)
			row := base("exponential_histogram", dp.Attributes(), dp.StartTimestamp(), dp.Timestamp(), dp.Flags(), dp.Exemplars())
			count, scale, zeroCount, zeroThreshold := dp.Count(), dp.Scale(), dp.ZeroCount(), dp.ZeroThreshold()
			positiveOffset, negativeOffset := dp.Positive().Offset(), dp.Negative().Offset()
			row.HistogramCount, row.AggregationTemporality, row.ExpScale, row.ExpZeroCount, row.ExpZeroThreshold, row.ExpPositiveOffset, row.ExpNegativeOffset = &count, temporality, &scale, &zeroCount, &zeroThreshold, &positiveOffset, &negativeOffset
			if dp.HasSum() {
				v := dp.Sum()
				row.HistogramSum = &v
			}
			if dp.HasMin() {
				v := dp.Min()
				row.HistogramMin = &v
			}
			if dp.HasMax() {
				v := dp.Max()
				row.HistogramMax = &v
			}
			row.ExpPositiveCounts, row.ExpNegativeCounts = uint64SliceJSON(dp.Positive().BucketCounts()), uint64SliceJSON(dp.Negative().BucketCounts())
			if err := store(row); err != nil {
				return err
			}
		}
	case pmetric.MetricTypeSummary:
		for i := 0; i < m.Summary().DataPoints().Len(); i++ {
			dp := m.Summary().DataPoints().At(i)
			row := base("summary", dp.Attributes(), dp.StartTimestamp(), dp.Timestamp(), dp.Flags(), pmetric.NewExemplarSlice())
			count, sum := dp.Count(), dp.Sum()
			row.SummaryCount, row.SummarySum, row.SummaryQuantiles = &count, &sum, summaryQuantilesJSON(dp)
			if err := store(row); err != nil {
				return err
			}
		}
	}
	return nil
}

func numericValue(dp pmetric.NumberDataPoint) float64 {
	if dp.ValueType() == pmetric.NumberDataPointValueTypeInt {
		return float64(dp.IntValue())
	}
	return dp.DoubleValue()
}
func floatSliceJSON(s pcommon.Float64Slice) string {
	out := make([]float64, s.Len())
	for i := range out {
		out[i] = s.At(i)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
func uint64SliceJSON(s pcommon.UInt64Slice) string {
	out := make([]uint64, s.Len())
	for i := range out {
		out[i] = s.At(i)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
func summaryQuantilesJSON(dp pmetric.SummaryDataPoint) string {
	out := make(map[string]float64, dp.QuantileValues().Len())
	for i := 0; i < dp.QuantileValues().Len(); i++ {
		q := dp.QuantileValues().At(i)
		out[fmt.Sprintf("%g", q.Quantile())] = q.Value()
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func metricAttributes(attrs pcommon.Map) (string, string, int) {
	all, indexed := make(map[string]any, attrs.Len()), map[string]any{}
	limited := 0
	attrs.Range(func(k string, v pcommon.Value) bool {
		all[k] = v.AsRaw()
		if _, ok := indexedMetricAttributes[k]; ok {
			indexed[k] = v.AsRaw()
		} else {
			limited++
		}
		return true
	})
	allJSON, _ := json.Marshal(all)
	indexedJSON, _ := json.Marshal(indexed)
	return string(allJSON), string(indexedJSON), limited
}

func exemplarsToJSON(exemplars pmetric.ExemplarSlice) string {
	if exemplars.Len() == 0 {
		return ""
	}
	type exemplar struct {
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
	out := make([]exemplar, 0, exemplars.Len())
	for i := 0; i < exemplars.Len(); i++ {
		ex := exemplars.At(i)
		if traceID := ex.TraceID(); !traceID.IsEmpty() {
			out = append(out, exemplar{TraceID: traceID.String(), SpanID: ex.SpanID().String()})
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// Keep the old exported helper for callers/tests. Query paths use raw buckets;
// this is only a utility and is never invoked while storing a metric point.
func HistogramPercentile(bounds []float64, counts []uint64, p float64) float64 {
	var total uint64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	target, cumulative := float64(total)*p, float64(0)
	for i, c := range counts {
		next := cumulative + float64(c)
		if next >= target {
			if i >= len(bounds) {
				if len(bounds) == 0 {
					return 0
				}
				return bounds[len(bounds)-1]
			}
			lo := 0.0
			if i > 0 {
				lo = bounds[i-1]
			}
			if c == 0 {
				return bounds[i]
			}
			return lo + (target-cumulative)/float64(c)*(bounds[i]-lo)
		}
		cumulative = next
	}
	return 0
}
