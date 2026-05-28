package ingestion

import (
	"math"
	"testing"
)

func nearly(t *testing.T, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("got %v, want %v (tol %v)", got, want, tol)
	}
}

func TestHistogramPercentile_Empty(t *testing.T) {
	if v := HistogramPercentile(nil, nil, 0.5); v != 0 {
		t.Errorf("empty histogram → %v, want 0", v)
	}
}

func TestHistogramPercentile_AllInFirstBucket(t *testing.T) {
	// 100 observations all in (0, 10] → p95 interpolates to 9.5.
	bounds := []float64{10, 20, 50}
	counts := []uint64{100, 0, 0, 0}
	nearly(t, HistogramPercentile(bounds, counts, 0.95), 9.5, 0.001)
	nearly(t, HistogramPercentile(bounds, counts, 0.50), 5.0, 0.001)
}

func TestHistogramPercentile_MultiBucketP95(t *testing.T) {
	// Bounds: 0–10, 10–20, 20–50, 50+. Counts: 90, 5, 4, 1 (total 100).
	// p95 → index 95. Cumulative: 90 (after first), 95 (after second).
	// We land exactly at the right edge of the (10, 20] bucket → 20.0.
	bounds := []float64{10, 20, 50}
	counts := []uint64{90, 5, 4, 1}
	nearly(t, HistogramPercentile(bounds, counts, 0.95), 20.0, 0.001)
}

func TestHistogramPercentile_MidBucketInterpolation(t *testing.T) {
	// Bounds 10/20/30. Counts 25/25/25/25 (total 100). p50 → target=50.
	// After bucket 0 cum=25, after bucket 1 cum=50 → we land at the right
	// edge of (10, 20] via frac=(50-25)/25=1.0 → 10 + 1.0*(20-10) = 20.
	bounds := []float64{10, 20, 30}
	counts := []uint64{25, 25, 25, 25}
	nearly(t, HistogramPercentile(bounds, counts, 0.50), 20.0, 0.001)

	// p40 → target=40, lands mid-bucket-1: 10 + (40-25)/25 * 10 = 16.
	nearly(t, HistogramPercentile(bounds, counts, 0.40), 16.0, 0.001)
}

func TestHistogramPercentile_OverflowBucket(t *testing.T) {
	// Most observations in the implicit (50, ∞) overflow → returns the last
	// explicit bound, which is the most useful approximation available.
	bounds := []float64{10, 50}
	counts := []uint64{1, 1, 100}
	if v := HistogramPercentile(bounds, counts, 0.99); v != 50 {
		t.Errorf("overflow bucket → %v, want 50 (last bound)", v)
	}
}

func TestHistogramPercentile_MonotonicAcrossPercentiles(t *testing.T) {
	// p50 ≤ p95 ≤ p99 must hold for any well-formed histogram.
	bounds := []float64{10, 25, 50, 100, 250, 500}
	counts := []uint64{40, 30, 15, 8, 5, 2, 0}
	p50 := HistogramPercentile(bounds, counts, 0.50)
	p95 := HistogramPercentile(bounds, counts, 0.95)
	p99 := HistogramPercentile(bounds, counts, 0.99)
	if !(p50 <= p95 && p95 <= p99) {
		t.Errorf("percentile ordering violated: p50=%v p95=%v p99=%v", p50, p95, p99)
	}
}

