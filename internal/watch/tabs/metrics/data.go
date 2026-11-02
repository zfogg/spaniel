package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
)

const sampleLimit = 2048
const seriesLimit = 256

// These presentation types normalize the anonymous generated HTTP point types.
// They only consume API fields; storage types are never used by the feature.
type sample struct {
	Timestamp         int64    `json:"timestamp_ns"`
	Start             int64    `json:"start_timestamp_ns"`
	Flags             uint32   `json:"flags"`
	Value             float64  `json:"value"`
	Count             *float64 `json:"count"`
	Sum, Min, Max     *float64
	Bounds, Buckets   []float64
	Quantiles         map[string]float64
	ExpScale          *float64       `json:"exp_scale"`
	ExpZeroCount      *float64       `json:"exp_zero_count"`
	ExpZeroThreshold  *float64       `json:"exp_zero_threshold"`
	ExpPositiveOffset *float64       `json:"exp_positive_offset"`
	ExpNegativeOffset *float64       `json:"exp_negative_offset"`
	ExpPositiveCounts []float64      `json:"exp_positive_counts"`
	ExpNegativeCounts []float64      `json:"exp_negative_counts"`
	ScopeName         string         `json:"scope_name"`
	ScopeVersion      string         `json:"scope_version"`
	ScopeAttributes   map[string]any `json:"scope_attributes"`
	Exemplars         []struct {
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
}
type labeledSeries struct {
	Key        string
	Attributes map[string]any
	Points     []sample
}
type seriesData struct {
	Name                    string
	Service                 string `json:"service_name"`
	Type, Unit, Description string
	Temporality             string `json:"aggregation_temporality"`
	Monotonic               *bool  `json:"is_monotonic"`
	Dimensions              map[string][]string
	Series                  []labeledSeries
	Points                  []sample
	Capped                  bool `json:"-"`
}

func normalize(value apigen.MetricSeries) (seriesData, error) {
	var out seriesData
	b, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	err = jsonDecode(string(b), &out)
	// Older APIs expose just the single-series points array.
	if len(out.Series) == 0 && len(out.Points) > 0 {
		out.Series = []labeledSeries{{Key: "legacy", Attributes: map[string]any{}, Points: out.Points}}
	}
	out.Points = nil
	return out, err
}
func (d *seriesData) bound(from, to int64) {
	sort.Slice(d.Series, func(i, j int) bool { return d.Series[i].Key < d.Series[j].Key })
	if len(d.Series) > seriesLimit {
		d.Capped = true
		d.Series = d.Series[:seriesLimit]
	}
	for i := range d.Series {
		s := &d.Series[i]
		byTime := make(map[int64]sample, len(s.Points))
		for _, p := range s.Points {
			if p.Timestamp >= from && p.Timestamp <= to {
				byTime[p.Timestamp] = p
			}
		}
		s.Points = make([]sample, 0, len(byTime))
		for _, p := range byTime {
			s.Points = append(s.Points, p)
		}
		sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].Timestamp < s.Points[j].Timestamp })
		if len(s.Points) > sampleLimit {
			d.Capped = true
			s.Points = s.Points[len(s.Points)-sampleLimit:]
		}
	}
	// Expired labels must not remain selectable as apparently current series.
	kept := d.Series[:0]
	for _, s := range d.Series {
		if len(s.Points) > 0 {
			kept = append(kept, s)
		}
	}
	d.Series = kept
}
func metricRow(event liveevent.Message, session string) *liveevent.MetricRowPayload {
	if event.Type != "metric" {
		return nil
	}
	payload, ok := event.Decoded.(*liveevent.MetricPayload)
	if !ok {
		var p liveevent.MetricPayload
		if json.Unmarshal(event.Payload, &p) != nil {
			return nil
		}
		payload = &p
	}
	row, ok := payload.Row.(*liveevent.MetricRowPayload)
	if !ok || row == nil || row.Name == "" || row.TimestampNs <= 0 {
		return nil
	}
	if session != "" && row.SessionID != session {
		return nil
	}
	return row
}
func rowSample(row *liveevent.MetricRowPayload) sample {
	p := sample{Timestamp: row.TimestampNs, Start: row.StartTimestampNs, Flags: row.Flags, Value: row.Value, Sum: row.HistogramSum, Min: row.HistogramMin, Max: row.HistogramMax, ScopeName: row.ScopeName, ScopeVersion: row.ScopeVersion}
	count := row.HistogramCount
	if count == nil {
		count = row.SummaryCount
	}
	if count != nil {
		v := float64(*count)
		p.Count = &v
	}
	if p.Sum == nil {
		p.Sum = row.SummarySum
	}
	p.ExpScale = asFloat(row.ExpScale)
	p.ExpZeroCount = asFloat(row.ExpZeroCount)
	p.ExpZeroThreshold = row.ExpZeroThreshold
	p.ExpPositiveOffset = asFloat(row.ExpPositiveOffset)
	p.ExpNegativeOffset = asFloat(row.ExpNegativeOffset)
	_ = json.Unmarshal([]byte(row.ExplicitBounds), &p.Bounds)
	_ = json.Unmarshal([]byte(row.BucketCounts), &p.Buckets)
	_ = json.Unmarshal([]byte(row.SummaryQuantiles), &p.Quantiles)
	_ = json.Unmarshal([]byte(row.ExpPositiveCounts), &p.ExpPositiveCounts)
	_ = json.Unmarshal([]byte(row.ExpNegativeCounts), &p.ExpNegativeCounts)
	_ = jsonDecode(row.ScopeAttributes, &p.ScopeAttributes)
	_ = json.Unmarshal([]byte(row.Exemplars), &p.Exemplars)
	return p
}
func asFloat[T ~int32 | ~uint64](v *T) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}
func number(v float64) string {
	if !finite(v) {
		return "unavailable"
	}
	return strconv.FormatFloat(v, 'g', 8, 64)
}
func optional(v *float64) string {
	if v == nil {
		return "unavailable"
	}
	return number(*v)
}
func finite(v float64) bool     { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func jsonText(value any) string { b, _ := json.Marshal(value); return safe(string(b)) }
func operations(typ string) []string {
	switch typ {
	case "sum", "counter":
		return []string{"raw", "delta", "rate"}
	case "gauge":
		return []string{"raw", "last", "min", "max", "avg"}
	case "histogram":
		return []string{"raw", "avg", "p50", "p90", "p95", "p99"}
	default:
		return []string{"raw"}
	}
}
func defaultOperation(row apigen.MetricCatalogEntry) string {
	if row.Type != "histogram" {
		return "raw"
	}
	if row.Name == "http.server.request.body.size" || row.Name == "http.server.response.body.size" {
		return "avg"
	}
	return "p95"
}

// plottedValue never turns a missing baseline, missing field or OTLP no-value
// flag into zero. The raw snapshot remains unchanged for the sample table.
func plottedValue(points []sample, index int, d seriesData, op string) (float64, bool) {
	p := points[index]
	if p.Flags&1 != 0 {
		return 0, false
	}
	switch d.Type {
	case "gauge":
		if !finite(p.Value) {
			return 0, false
		}
		v := p.Value
		if op == "avg" || op == "min" || op == "max" {
			total, count := 0.0, 0
			for _, prior := range points[:index+1] {
				if prior.Flags&1 != 0 || !finite(prior.Value) {
					continue
				}
				total += prior.Value
				count++
				if op == "min" {
					v = math.Min(v, prior.Value)
				}
				if op == "max" {
					v = math.Max(v, prior.Value)
				}
			}
			if op == "avg" {
				v = total / float64(count)
			}
		}
		return v, finite(v)
	case "sum", "counter":
		if op == "raw" {
			return p.Value, finite(p.Value)
		}
		v := p.Value
		interval := p.Timestamp - p.Start
		switch strings.ToLower(d.Temporality) {
		case "cumulative":
			if index == 0 {
				return 0, false
			}
			prev := points[index-1]
			if prev.Flags&1 != 0 || !finite(prev.Value) || p.Timestamp <= prev.Timestamp {
				return 0, false
			}
			interval = p.Timestamp - prev.Timestamp
			reset := p.Start != 0 && p.Start != prev.Start
			if d.Monotonic != nil && *d.Monotonic && p.Value < prev.Value {
				reset = true
			}
			if reset {
				if p.Start > prev.Timestamp && p.Start < p.Timestamp {
					interval = p.Timestamp - p.Start
				}
			} else {
				v -= prev.Value
			}
		case "delta":
			if p.Start <= 0 || interval <= 0 {
				if op == "rate" {
					return 0, false
				}
			}
		default:
			return 0, false
		}
		if op == "rate" {
			if interval <= 0 {
				return 0, false
			}
			v /= float64(interval) / 1e9
		}
		return v, finite(v)
	case "histogram":
		if op == "raw" {
			return 0, false
		}
		count, sum, buckets, ok := histogramInterval(points, index, d.Temporality)
		if !ok || count <= 0 {
			return 0, false
		}
		if op == "avg" {
			if sum == nil {
				return 0, false
			}
			v := *sum / count
			return v, finite(v)
		}
		q, err := strconv.ParseFloat(strings.TrimPrefix(op, "p"), 64)
		if err != nil || len(buckets) != len(p.Bounds)+1 || len(p.Bounds) == 0 {
			return 0, false
		}
		total := 0.0
		for _, n := range buckets {
			if !finite(n) || n < 0 {
				return 0, false
			}
			total += n
		}
		if total <= 0 {
			return 0, false
		}
		cumulative := 0.0
		target := total * q / 100
		for i, n := range buckets {
			if cumulative+n >= target {
				if i >= len(p.Bounds) {
					return p.Bounds[len(p.Bounds)-1], finite(p.Bounds[len(p.Bounds)-1])
				}
				lo := 0.0
				if i > 0 {
					lo = p.Bounds[i-1]
				}
				if n <= 0 || !finite(p.Bounds[i]) || p.Bounds[i] < lo {
					return 0, false
				}
				v := lo + (target-cumulative)/n*(p.Bounds[i]-lo)
				return v, finite(v)
			}
			cumulative += n
		}
	}
	return 0, false
}
func histogramInterval(points []sample, index int, temporality string) (float64, *float64, []float64, bool) {
	p := points[index]
	if p.Count == nil || !finite(*p.Count) {
		return 0, nil, nil, false
	}
	count, sum := *p.Count, p.Sum
	buckets := append([]float64(nil), p.Buckets...)
	if strings.EqualFold(temporality, "Cumulative") && index > 0 {
		prev := points[index-1]
		reset := prev.Count == nil || prev.Flags&1 != 0 || count < *prev.Count || (p.Start != 0 && p.Start != prev.Start) || len(p.Bounds) != len(prev.Bounds) || len(buckets) != len(prev.Buckets)
		for i, b := range p.Bounds {
			if !reset && b != prev.Bounds[i] {
				reset = true
			}
		}
		for i, b := range buckets {
			if !reset && b < prev.Buckets[i] {
				reset = true
			}
		}
		if !reset {
			count -= *prev.Count
			if sum != nil && prev.Sum != nil {
				v := *sum - *prev.Sum
				sum = &v
			} else {
				sum = nil
			}
			for i := range buckets {
				buckets[i] -= prev.Buckets[i]
			}
		}
	}
	return count, sum, buckets, true
}
func sampleDescription(p sample, typ string, width int) []string {
	lines := []string{fmt.Sprintf("timestamp %s · start %s · flags %d", timestamp(p.Timestamp), timestamp(p.Start), p.Flags)}
	if typ == "gauge" || typ == "sum" || typ == "counter" {
		lines = append(lines, "raw value "+number(p.Value))
	} else {
		lines = append(lines, fmt.Sprintf("count %s · sum %s · min %s · max %s", optional(p.Count), optional(p.Sum), optional(p.Min), optional(p.Max)))
		if typ == "histogram" {
			lines = append(lines, "bounds "+jsonText(p.Bounds), "bucket counts "+jsonText(p.Buckets))
		}
		if typ == "exponential_histogram" {
			lines = append(lines, fmt.Sprintf("scale %s · zero count %s · threshold %s", optional(p.ExpScale), optional(p.ExpZeroCount), optional(p.ExpZeroThreshold)), "positive offset "+optional(p.ExpPositiveOffset)+" counts "+jsonText(p.ExpPositiveCounts), "negative offset "+optional(p.ExpNegativeOffset)+" counts "+jsonText(p.ExpNegativeCounts))
		}
		if typ == "summary" {
			lines = append(lines, "quantiles "+jsonText(p.Quantiles))
		}
	}
	if p.Flags&1 != 0 {
		lines = append(lines, "OTLP no recorded value: excluded from chart")
	}
	if p.ScopeName != "" {
		lines = append(lines, "scope "+safe(p.ScopeName)+" "+safe(p.ScopeVersion))
		lines = append(lines, attributeTable("Scope attributes", p.ScopeAttributes, width)...)
	}
	for _, ex := range p.Exemplars {
		lines = append(lines, "exemplar trace="+safe(ex.TraceID)+" span="+safe(ex.SpanID))
	}
	return lines
}

func jsonDecode(s string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(s))
	decoder.UseNumber()
	return decoder.Decode(v)
}
