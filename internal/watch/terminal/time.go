package terminal

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// BatchTime supplies one reference instant for a complete formatting batch.
func BatchTime(now ...time.Time) time.Time {
	if len(now) > 0 && !now[0].IsZero() {
		return now[0]
	}
	return time.Now()
}

func Relative(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	if d == 0 {
		return "0s ago"
	}
	future := d < 0
	if future {
		d = -d
	}
	value := "<1s"
	switch {
	case d >= 24*time.Hour:
		value = fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour:
		value = fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Minute:
		value = fmt.Sprintf("%dm", d/time.Minute)
	case d >= time.Second:
		value = fmt.Sprintf("%ds", d/time.Second)
	}
	if future {
		return "in " + value
	}
	return value + " ago"
}

func Timestamp(ns int64, layout string, zone *time.Location, now time.Time) string {
	if ns == 0 {
		return "—"
	}
	t := time.Unix(0, ns)
	if zone != nil {
		t = t.In(zone)
	}
	return t.Format(layout) + " * " + Relative(t, now)
}

// FitTimestamp keeps the relative suffix useful when an absolute timestamp
// needs an ellipsis in a narrow cell. Attribute/result values use full wrapping.
func FitTimestamp(value string, width int) string {
	width = max(1, width)
	if ansi.StringWidth(value) <= width {
		return value
	}
	if at := strings.LastIndex(value, " * "); at >= 0 {
		age := value[at+3:]
		if strings.HasSuffix(age, " ago") || strings.HasPrefix(age, "in ") {
			suffix := " * " + age
			room := width - ansi.StringWidth(suffix)
			if room > 0 {
				return ansi.Truncate(value[:at], room, "…") + suffix
			}
			return ansi.Truncate(age, width, "…")
		}
	}
	return ansi.Truncate(value, width, "…")
}
