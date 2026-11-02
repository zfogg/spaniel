package charts

import (
	"strings"
	"testing"
	"time"
)

func TestIntradayAxisShowsUsefulUTCTimes(t *testing.T) {
	from := time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)
	chart := NewTimeSeries(100, 12, from, from.Add(15*time.Minute), 0, 10)
	if got := chart.XLabelFormatter(0, float64(from.Add(7*time.Minute).Unix())); got != "14:07:00" {
		t.Fatalf("intraday tick: %s", got)
	}
	view := RenderTimeSeries(100, 12, from, from.Add(15*time.Minute), []Series{{Name: "real", Points: []Point{{from, 1}, {from.Add(10 * time.Minute), 9}}}})
	if !strings.Contains(view, ":") || strings.Contains(view, "179363") {
		t.Fatal(view)
	}
	long := NewTimeSeries(100, 12, from, from.Add(7*24*time.Hour), 0, 10)
	if got := long.XLabelFormatter(0, float64(from.Unix())); !strings.Contains(got, "11/02") {
		t.Fatal("multi-day date context lost", got)
	}
}
