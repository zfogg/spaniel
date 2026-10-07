package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
)

func (r *Router) listMetrics(w http.ResponseWriter, req *http.Request) {
	sessionID := r.scopeSession(req.URL.Query().Get("sessionId"))
	entries, err := r.store.WithContext(req.Context()).ListMetricCatalog(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if entries == nil {
		entries = []*storage.MetricCatalogEntry{}
	}
	respond(w, entries, len(entries), 1)
}

// MetricCardinalityStream is the durable admission-budget state for one
// metric stream. Arbitrary point attributes never appear here.
type MetricCardinalityStream struct {
	ServiceName string `json:"service_name"`
	Name        string `json:"name"`
	Active      int    `json:"active_series"`
	Limit       int    `json:"limit"`
}

// GET /api/metrics/cardinality?sessionId=
// This makes the trusted, persisted series budget inspectable without turning
// untrusted OTLP attributes into an index or public query dimension.
func (r *Router) getMetricCardinality(w http.ResponseWriter, req *http.Request) {
	sessionID := r.scopeSession(req.URL.Query().Get("sessionId"))
	rows, err := r.store.WithContext(req.Context()).MetricSeriesCatalog()
	if err != nil {
		respondErr(w, req, http.StatusInternalServerError, err.Error())
		return
	}
	byStream := map[string]*MetricCardinalityStream{}
	for _, row := range rows {
		if sessionID != "" && row.SessionID != sessionID {
			continue
		}
		key := row.Service + "\x00" + row.Name
		stream := byStream[key]
		if stream == nil {
			stream = &MetricCardinalityStream{ServiceName: row.Service, Name: row.Name, Limit: 2000}
			byStream[key] = stream
		}
		stream.Active++
	}
	out := make([]MetricCardinalityStream, 0, len(byStream))
	for _, stream := range byStream {
		out = append(out, *stream)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServiceName == out[j].ServiceName {
			return out[i].Name < out[j].Name
		}
		return out[i].ServiceName < out[j].ServiceName
	})
	respond(w, out, len(out), 1)
}

// MetricSeriesPoint is an OTLP point plus an optional query-time derived value.
// Histogram buckets are returned verbatim so quantiles and heatmaps are based on
// observations rather than ingest-time estimates.
type MetricSeriesPoint struct {
	StartTimestampNs  int64                  `json:"start_timestamp_ns,omitempty"`
	TimestampNs       int64                  `json:"timestamp_ns"`
	Flags             uint32                 `json:"flags,omitempty"`
	Value             float64                `json:"value"`
	Count             *uint64                `json:"count,omitempty"`
	Sum               *float64               `json:"sum,omitempty"`
	Min               *float64               `json:"min,omitempty"`
	Max               *float64               `json:"max,omitempty"`
	Bounds            []float64              `json:"bounds,omitempty"`
	Buckets           []uint64               `json:"buckets,omitempty"`
	Quantiles         map[string]float64     `json:"quantiles,omitempty"`
	ExpScale          *int32                 `json:"exp_scale,omitempty"`
	ExpZeroCount      *uint64                `json:"exp_zero_count,omitempty"`
	ExpZeroThreshold  *float64               `json:"exp_zero_threshold,omitempty"`
	ExpPositiveOffset *int32                 `json:"exp_positive_offset,omitempty"`
	ExpPositiveCounts []uint64               `json:"exp_positive_counts,omitempty"`
	ExpNegativeOffset *int32                 `json:"exp_negative_offset,omitempty"`
	ExpNegativeCounts []uint64               `json:"exp_negative_counts,omitempty"`
	ScopeName         string                 `json:"scope_name,omitempty"`
	ScopeVersion      string                 `json:"scope_version,omitempty"`
	ScopeSchemaURL    string                 `json:"scope_schema_url,omitempty"`
	ScopeAttributes   map[string]any         `json:"scope_attributes,omitempty"`
	Exemplars         []MetricSeriesExemplar `json:"exemplars,omitempty"`
}
type MetricSeriesExemplar struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}
type MetricSeries struct {
	Key        string              `json:"key"`
	Attributes map[string]any      `json:"attributes"`
	Points     []MetricSeriesPoint `json:"points"`
}
type MetricSeriesResponse struct {
	Name                   string                  `json:"name"`
	ServiceName            string                  `json:"service_name"`
	Type                   string                  `json:"type"`
	Unit                   string                  `json:"unit"`
	Description            string                  `json:"description"`
	AggregationTemporality string                  `json:"aggregation_temporality,omitempty"`
	IsMonotonic            *bool                   `json:"is_monotonic,omitempty"`
	Operation              string                  `json:"operation"`
	Aggregation            string                  `json:"aggregation"`
	Dimensions             map[string][]string     `json:"dimensions"`
	Series                 []MetricSeries          `json:"series"`
	Points                 []MetricSeriesPoint     `json:"points"`
	Traces                 []*storage.TraceOverlay `json:"traces"`
}

// GET /api/metrics/series?name=&service=&sessionId=&from=&to=&operation=
// Repeated attr.<allowed-key>=value parameters filter indexed dimensions. Raw
// OTLP attributes remain inspectable on the point but are never query keys.
func (r *Router) getMetricSeries(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	name := q.Get("name")
	if name == "" {
		respondErr(w, req, 400, "name query param required")
		return
	}
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	rows, err := r.store.WithContext(req.Context()).GetMetricSeries(storage.MetricSeriesFilter{Name: name, Service: q.Get("service"), SessionID: r.scopeSession(q.Get("sessionId")), FromNs: from, ToNs: to})
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	out := MetricSeriesResponse{Name: name, Operation: q.Get("operation"), Aggregation: "per_complete_attribute_set", Dimensions: map[string][]string{}, Series: []MetricSeries{}, Points: []MetricSeriesPoint{}, Traces: []*storage.TraceOverlay{}}
	if out.Operation == "" {
		out.Operation = "raw"
	}
	filters := metricDimensionFilters(q)
	byKey := map[string]*MetricSeries{}
	values := map[string]map[string]struct{}{}
	for _, row := range rows {
		attrs := map[string]any{}
		_ = json.Unmarshal([]byte(row.SeriesAttributes), &attrs)
		// Dimension menus describe the stream, not merely the currently filtered
		// result. That lets an operator replace one filter without first clearing
		// every other selected label.
		for k, v := range attrs {
			if values[k] == nil {
				values[k] = map[string]struct{}{}
			}
			values[k][stringifyMetricDimension(v)] = struct{}{}
		}
		if !matchesMetricFilters(attrs, filters) {
			continue
		}
		if out.ServiceName == "" {
			out.ServiceName, out.Type, out.Unit, out.Description, out.AggregationTemporality, out.IsMonotonic = row.ServiceName, row.Type, row.Unit, row.Description, row.AggregationTemporality, row.IsMonotonic
		}
		key := row.SeriesKey
		if key == "" {
			key = row.Name + "\x00" + row.ServiceName + "\x00" + row.SeriesAttributes
		}
		series := byKey[key]
		if series == nil {
			series = &MetricSeries{Key: key, Attributes: attrs, Points: []MetricSeriesPoint{}}
			byKey[key] = series
		}
		series.Points = append(series.Points, metricPoint(row))
	}
	for k, set := range values {
		for v := range set {
			out.Dimensions[k] = append(out.Dimensions[k], v)
		}
		sort.Strings(out.Dimensions[k])
	}
	for _, series := range byKey {
		deriveMetricSeries(series.Points, out.Type, out.AggregationTemporality, out.Operation)
		sort.Slice(series.Points, func(i, j int) bool { return series.Points[i].TimestampNs < series.Points[j].TimestampNs })
		out.Series = append(out.Series, *series)
	}
	sort.Slice(out.Series, func(i, j int) bool { return out.Series[i].Key < out.Series[j].Key })
	if len(out.Series) == 1 {
		out.Points = out.Series[0].Points
	}
	if len(rows) == 0 {
		metadata, err := r.store.WithContext(req.Context()).GetMetricStreamMetadata(storage.MetricSeriesFilter{Name: name, Service: q.Get("service"), SessionID: r.scopeSession(q.Get("sessionId"))})
		if err != nil {
			respondErr(w, req, 500, err.Error())
			return
		}
		if metadata != nil {
			out.ServiceName, out.Type, out.Unit, out.Description, out.AggregationTemporality, out.IsMonotonic = metadata.ServiceName, metadata.Type, metadata.Unit, metadata.Description, metadata.AggregationTemporality, metadata.IsMonotonic
		}
	}
	if q.Get("with_traces") == "1" && len(rows) > 0 {
		if from == 0 {
			from = rows[0].TimestampNs
		}
		if to == 0 {
			to = rows[len(rows)-1].TimestampNs
		}
		if traces, err := r.store.WithContext(req.Context()).ListTracesInWindow(storage.TraceOverlayFilter{Service: q.Get("service"), SessionID: r.scopeSession(q.Get("sessionId")), FromNs: from, ToNs: to}); err == nil && traces != nil {
			out.Traces = traces
		}
	}
	respond(w, out, len(out.Series), 1)
}

func metricPoint(row *storage.Metric) MetricSeriesPoint {
	p := MetricSeriesPoint{StartTimestampNs: row.StartTimestampNs, TimestampNs: row.TimestampNs, Flags: row.Flags, Value: row.Value, Count: row.HistogramCount, Sum: row.HistogramSum, Min: row.HistogramMin, Max: row.HistogramMax, ExpScale: row.ExpScale, ExpZeroCount: row.ExpZeroCount, ExpZeroThreshold: row.ExpZeroThreshold, ExpPositiveOffset: row.ExpPositiveOffset, ExpNegativeOffset: row.ExpNegativeOffset, ScopeName: row.ScopeName, ScopeVersion: row.ScopeVersion, ScopeSchemaURL: row.ScopeSchemaURL, ScopeAttributes: map[string]any{}, Exemplars: []MetricSeriesExemplar{}}
	_ = json.Unmarshal([]byte(row.ExplicitBounds), &p.Bounds)
	_ = json.Unmarshal([]byte(row.BucketCounts), &p.Buckets)
	_ = json.Unmarshal([]byte(row.SummaryQuantiles), &p.Quantiles)
	_ = json.Unmarshal([]byte(row.ExpPositiveCounts), &p.ExpPositiveCounts)
	_ = json.Unmarshal([]byte(row.ExpNegativeCounts), &p.ExpNegativeCounts)
	_ = json.Unmarshal([]byte(row.ScopeAttributes), &p.ScopeAttributes)
	_ = json.Unmarshal([]byte(row.Exemplars), &p.Exemplars)
	if p.Sum == nil && row.SummarySum != nil {
		p.Sum = row.SummarySum
	}
	if p.Count == nil && row.SummaryCount != nil {
		p.Count = row.SummaryCount
	}
	return p
}

func deriveMetricSeries(points []MetricSeriesPoint, typ, temporality, operation string) {
	if typ == "gauge" && operation != "raw" {
		var total float64
		for i := range points {
			total += points[i].Value
			switch operation {
			case "avg":
				points[i].Value = total / float64(i+1)
			case "min":
				if i > 0 && points[i].Value > points[i-1].Value {
					points[i].Value = points[i-1].Value
				}
			case "max":
				if i > 0 && points[i].Value < points[i-1].Value {
					points[i].Value = points[i-1].Value
				}
			case "last": // raw point value is already the last observation at its timestamp.
			}
		}
		return
	}
	if typ == "histogram" {
		// OTLP cumulative histograms are snapshots since process start. Charts need
		// the observations from each export interval, not the lifetime aggregate.
		// Keep raw points untouched and derive the interval values only for views.
		cumulative := temporality == "Cumulative"
		switch {
		case operation == "avg":
			// Histogram sums preserve the observed byte/duration values while
			// percentile queries are necessarily quantized to bucket boundaries.
			for i := range points {
				count, sum, _ := histogramInterval(points, i, cumulative)
				if count > 0 {
					points[i].Value = sum / float64(count)
				}
			}
		case strings.HasPrefix(operation, "p"):
			q, err := strconv.ParseFloat(strings.TrimPrefix(operation, "p"), 64)
			if err == nil {
				for i := range points {
					_, _, buckets := histogramInterval(points, i, cumulative)
					points[i].Value = histogramPercentile(points[i].Bounds, buckets, q/100)
				}
			}
		}
		return
	}
	if operation == "raw" || typ != "sum" {
		return
	}
	for i := range points {
		if temporality == "Cumulative" {
			if i == 0 {
				points[i].Value = 0
				continue
			}
			delta := points[i].Value - points[i-1].Value
			if delta < 0 || (points[i].StartTimestampNs != 0 && points[i].StartTimestampNs != points[i-1].StartTimestampNs) {
				delta = points[i].Value
			}
			points[i].Value = delta
		}
		if operation == "rate" {
			interval := points[i].TimestampNs - points[i].StartTimestampNs
			if temporality == "Cumulative" && i > 0 {
				interval = points[i].TimestampNs - points[i-1].TimestampNs
			}
			if interval > 0 {
				points[i].Value /= float64(interval) / 1e9
			} else {
				points[i].Value = 0
			}
		}
	}
}

func histogramInterval(points []MetricSeriesPoint, index int, cumulative bool) (uint64, float64, []uint64) {
	current := points[index]
	var count uint64
	if current.Count != nil {
		count = *current.Count
	}
	var sum float64
	if current.Sum != nil {
		sum = *current.Sum
	}
	buckets := append([]uint64(nil), current.Buckets...)
	if !cumulative || index == 0 {
		return count, sum, buckets
	}
	previous := points[index-1]
	reset := previous.Count == nil || previous.Sum == nil || current.Count == nil || current.Sum == nil ||
		count < *previous.Count || sum < *previous.Sum ||
		(current.StartTimestampNs != 0 && current.StartTimestampNs != previous.StartTimestampNs) ||
		len(buckets) != len(previous.Buckets)
	if reset {
		return count, sum, buckets
	}
	for i := range buckets {
		if buckets[i] < previous.Buckets[i] {
			return count, sum, append([]uint64(nil), current.Buckets...)
		}
		buckets[i] -= previous.Buckets[i]
	}
	return count - *previous.Count, sum - *previous.Sum, buckets
}

func metricDimensionFilters(q map[string][]string) map[string]string {
	out := map[string]string{}
	for k, v := range q {
		if strings.HasPrefix(k, "attr.") && len(v) > 0 {
			out[strings.TrimPrefix(k, "attr.")] = v[0]
		}
	}
	return out
}
func matchesMetricFilters(attrs map[string]any, filters map[string]string) bool {
	for k, want := range filters {
		got, ok := attrs[k]
		if !ok || stringifyMetricDimension(got) != want {
			return false
		}
	}
	return true
}
func stringifyMetricDimension(v any) string {
	return strings.TrimSpace(strings.Trim(fmtSprint(v), "\""))
}
func fmtSprint(v any) string {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return ""
	}
	if b[0] == '"' {
		var s string
		_ = json.Unmarshal(b, &s)
		return s
	}
	return string(b)
}

func histogramPercentile(bounds []float64, counts []uint64, p float64) float64 {
	var total uint64
	for _, count := range counts {
		total += count
	}
	if total == 0 {
		return 0
	}
	target, cumulative := float64(total)*p, float64(0)
	for i, count := range counts {
		next := cumulative + float64(count)
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
			if count == 0 {
				return bounds[i]
			}
			return lo + (target-cumulative)/float64(count)*(bounds[i]-lo)
		}
		cumulative = next
	}
	return 0
}
