package metrics

import (
	"math"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/api/apigen"
)

func ptr[T any](v T) *T { return &v }
func TestCounterViewsRespectIntervalsResetsAndNoValue(t *testing.T) {
	d := seriesData{Type: "sum", Temporality: "Cumulative", Monotonic: ptr(true)}
	points := []sample{{Timestamp: 10e9, Start: 1e9, Value: 100}, {Timestamp: 20e9, Start: 1e9, Value: 130}, {Timestamp: 30e9, Start: 1e9, Value: 180}, {Timestamp: 40e9, Start: 35e9, Value: 20}, {Timestamp: 50e9, Start: 35e9, Value: 50, Flags: 1}, {Timestamp: 60e9, Start: 35e9, Value: 90}}
	for _, tc := range []struct {
		index int
		op    string
		want  float64
		valid bool
	}{{0, "raw", 100, true}, {0, "delta", 0, false}, {0, "rate", 0, false}, {1, "delta", 30, true}, {1, "rate", 3, true}, {2, "delta", 50, true}, {2, "rate", 5, true}, {3, "delta", 20, true}, {3, "rate", 4, true}, {4, "raw", 0, false}, {5, "rate", 0, false}} {
		got, ok := plottedValue(points, tc.index, d, tc.op)
		if ok != tc.valid || (ok && got != tc.want) {
			t.Errorf("%s[%d] = %g,%t want %g,%t", tc.op, tc.index, got, ok, tc.want, tc.valid)
		}
	}
	if points[1].Value != 130 {
		t.Fatal("derivation mutated authoritative observations")
	}
	d.Monotonic = ptr(false)
	points = []sample{{Timestamp: 10e9, Value: 15}, {Timestamp: 20e9, Value: 5}}
	if v, ok := plottedValue(points, 1, d, "delta"); !ok || v != -10 {
		t.Fatalf("nonmonotonic delta = %v,%v", v, ok)
	}
	d.Temporality = "Delta"
	points = []sample{{Timestamp: 20e9, Start: 15e9, Value: 40}, {Timestamp: 30e9, Value: 50}}
	if v, ok := plottedValue(points, 0, d, "rate"); !ok || v != 8 {
		t.Fatalf("delta rate = %v,%v", v, ok)
	}
	if _, ok := plottedValue(points, 1, d, "rate"); ok {
		t.Fatal("rate without interval plotted")
	}
	d.Temporality = ""
	if _, ok := plottedValue(points, 0, d, "delta"); ok {
		t.Fatal("unknown temporality silently treated as delta")
	}
}
func TestHistogramViewsUseRawIntervalFields(t *testing.T) {
	d := seriesData{Type: "histogram", Temporality: "Cumulative"}
	points := []sample{{Timestamp: 10, Count: ptr(10.0), Sum: ptr(100.0), Bounds: []float64{10, 20}, Buckets: []float64{5, 5, 0}}, {Timestamp: 20, Count: ptr(14.0), Sum: ptr(160.0), Bounds: []float64{10, 20}, Buckets: []float64{5, 9, 0}}, {Timestamp: 30, Count: ptr(14.0), Sum: ptr(160.0), Bounds: []float64{10, 20}, Buckets: []float64{5, 9, 0}}}
	if v, ok := plottedValue(points, 1, d, "avg"); !ok || v != 15 {
		t.Fatalf("interval mean %v,%v", v, ok)
	}
	if v, ok := plottedValue(points, 1, d, "p95"); !ok || v != 19.5 {
		t.Fatalf("interval p95 %v,%v", v, ok)
	}
	if _, ok := plottedValue(points, 2, d, "avg"); ok {
		t.Fatal("empty histogram interval plotted as zero")
	}
	if _, ok := plottedValue(points, 1, d, "raw"); ok {
		t.Fatal("raw histogram treated as scalar")
	}
	points[1].Sum = nil
	if _, ok := plottedValue(points, 1, d, "avg"); ok {
		t.Fatal("missing sum treated as zero")
	}
	points[1].Buckets = nil
	if _, ok := plottedValue(points, 1, d, "p95"); ok {
		t.Fatal("missing buckets plotted")
	}
	for _, typ := range []string{"summary", "exponential_histogram", "future_type"} {
		d.Type = typ
		if _, ok := plottedValue(points, 0, d, "raw"); ok {
			t.Fatalf("%s treated as scalar", typ)
		}
	}
}
func TestGaugeNoValueAndNonfiniteExcluded(t *testing.T) {
	d := seriesData{Type: "gauge"}
	points := []sample{{Value: 4}, {Value: 0, Flags: 1}, {Value: 8}, {Value: math.NaN()}, {Value: math.Inf(1)}}
	if v, ok := plottedValue(points, 2, d, "avg"); !ok || v != 6 {
		t.Fatalf("mean %v,%v", v, ok)
	}
	for _, i := range []int{1, 3, 4} {
		if _, ok := plottedValue(points, i, d, "raw"); ok {
			t.Fatalf("invalid point %d plotted", i)
		}
	}
}
func TestGeneratedHTTPNormalizationPreservesDistributions(t *testing.T) {
	var raw apigen.MetricSeries
	err := jsonDecode(`{"name":"distribution","type":"exponential_histogram","unit":"ms","aggregation_temporality":"Delta","series":[{"key":"one","attributes":{"route":"/cart"},"points":[{"timestamp_ns":1793631000000000001,"count":3,"sum":24,"exp_scale":2,"exp_zero_count":1,"exp_positive_counts":[1,1],"quantiles":{"0.95":15},"flags":1,"scope_name":"sdk","exemplars":[{"trace_id":"t","span_id":"s"}]}]}],"points":[],"traces":[]}`, &raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := data.Series[0].Points[0]
	if p.Timestamp != 1793631000000000001 || p.Count == nil || *p.Count != 3 || p.ExpScale == nil || *p.ExpScale != 2 || p.Quantiles["0.95"] != 15 || len(p.Exemplars) != 1 {
		t.Fatalf("lost API fields: %+v", p)
	}
	text := strings.Join(sampleDescription(p, "exponential_histogram", 100), "\n")
	for _, want := range []string{"scale 2", "positive offset", "[1,1]", "OTLP no recorded value", "exemplar trace=t span=s"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}
func TestBoundDeduplicatesSortsExpiresAndCaps(t *testing.T) {
	d := seriesData{Series: []labeledSeries{{Key: "a", Points: []sample{{Timestamp: 3, Value: 1}, {Timestamp: 2}, {Timestamp: 3, Value: 9}, {Timestamp: 9}}}, {Key: "expired", Points: []sample{{Timestamp: 1}}}}}
	d.bound(2, 5)
	if len(d.Series) != 1 || len(d.Series[0].Points) != 2 || d.Series[0].Points[0].Timestamp != 2 || d.Series[0].Points[1].Value != 9 {
		t.Fatalf("bounded data: %+v", d)
	}
	for i := int64(0); i < sampleLimit+100; i++ {
		d.Series[0].Points = append(d.Series[0].Points, sample{Timestamp: 10 + i})
	}
	d.bound(0, 10000)
	if !d.Capped || len(d.Series[0].Points) != sampleLimit {
		t.Fatalf("sample bound missing")
	}
	d.bound(20000, 30000)
	if len(d.Series) != 0 {
		t.Fatal("expired series remains current")
	}
}

func TestTimestampPresentationUsesReadableUTCMilliseconds(t *testing.T) {
	const precise int64 = 1793631000123456789
	if got := timestamp(precise); got != "2026-11-02 15:30:00.123Z" {
		t.Fatalf("timestamp presentation = %q", got)
	}
	if timestamp(0) != "—" {
		t.Fatal("missing timestamp should remain unavailable")
	}
	// Presentation is separate from exact point identity and calculation fields.
	point := sample{Timestamp: precise}
	_ = sampleDescription(point, "gauge", 100)
	if point.Timestamp != precise {
		t.Fatal("presentation changed internal precision")
	}
}
