package charts

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestIntradayAxisShowsUsefulUTCTimes(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "0")
	from := time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)
	chart := NewTimeSeries(100, 12, from, from.Add(15*time.Minute), 0, 10, from.Add(15*time.Minute))
	if got := chart.XLabelFormatter(0, float64(from.Add(7*time.Minute).Unix())); got != "14:07:00Z • 8m ago" {
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

func TestHistoricalSubsecondRangeUsesItsSamplesInsteadOfCurrentTime(t *testing.T) {
	from := time.Date(2026, 1, 2, 14, 7, 0, 100000000, time.UTC)
	to := from.Add(10 * time.Millisecond)
	chart := NewTimeSeries(80, 10, from, to, 0, 10)
	if chart.ViewMinX() != float64(from.Unix()) || chart.ViewMaxX() != float64(from.Unix()+1) {
		t.Fatal("subsecond display range collapsed", chart.ViewMinX(), chart.ViewMaxX())
	}
	view := RenderTimeSeries(80, 10, from, to, []Series{{Name: "historical", Points: []Point{{from, 1}, {to, 9}}}})
	if !strings.Contains(view, "14:07:00") || strings.IndexFunc(view, func(r rune) bool { return unicode.In(r, unicode.Braille) }) < 0 {
		t.Fatal("historical samples not drawn", view)
	}
	if to.Sub(from) != 10*time.Millisecond {
		t.Fatal("chart display normalization changed precise inputs")
	}
}
