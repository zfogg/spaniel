package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestRelativeTimeFixedClock(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "0")
	now := time.Date(2026, 11, 2, 21, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		offset time.Duration
		want   string
	}{{0, "0s ago"}, {-500 * time.Millisecond, "<1s ago"}, {-5 * time.Second, "5s ago"}, {-2 * time.Minute, "2m ago"}, {-3 * time.Hour, "3h ago"}, {-2 * 24 * time.Hour, "2d ago"}, {2 * time.Minute, "in 2m"}, {500 * time.Millisecond, "in <1s"}} {
		if got := Relative(now.Add(tc.offset), now); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	if Relative(time.Time{}, now) != "—" || Timestamp(0, "15:04", time.UTC, now) != "—" {
		t.Fatal("missing time acquired an age")
	}
	value := Timestamp(now.Add(-2*time.Minute).UnixNano(), "2006-01-02 15:04:05.000 MST", time.UTC, now)
	if value != "2026-11-02 21:03:00.000 UTC • 2m ago" {
		t.Fatal(value)
	}
	for _, width := range []int{1, 8, 18, 28, 60} {
		fitted := FitTimestamp(value, width)
		if ansi.StringWidth(fitted) > width || width >= 8 && !strings.Contains(fitted, "2m ago") {
			t.Fatal(width, fitted)
		}
	}
}
