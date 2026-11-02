package metadata

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMetadataStaysOnOneRowAndEllipsizes(t *testing.T) {
	fields := []Field{
		{Name: "Trace ID", Value: strings.Repeat("abcdef", 10)},
		{Name: "Span ID", Value: "1234567890abcdef"},
		{Name: "Timestamp", Value: "2026-11-02 21:03:00.000 UTC * 2m ago"},
		{Name: "Service", Value: "api\n\x1b[2J" + strings.Repeat("界", 20)},
	}
	for _, width := range []int{1, 4, 12, 24, 40, 120} {
		lines := Table(fields, width)
		if len(lines) != len(fields)+2 {
			t.Fatalf("width %d: metadata wrapped into extra rows", width)
		}
		for _, line := range lines {
			if strings.ContainsAny(ansi.Strip(line), "\n\r\t") || ansi.StringWidth(line) > width {
				t.Fatalf("width %d: unsafe or overflowing row %q", width, line)
			}
		}
		if width >= 24 && (!strings.Contains(lines[0], "FIELD") || !strings.Contains(lines[0], "VALUE")) {
			t.Fatalf("width %d: missing table headings", width)
		}
		if width >= 12 && width <= 40 && !strings.Contains(lines[2], "…") {
			t.Fatalf("width %d: long trace ID has no ellipsis: %q", width, lines[2])
		}
		if width >= 24 && !strings.Contains(lines[4], "2m ago") {
			t.Fatalf("width %d: timestamp lost relative age: %q", width, lines[4])
		}
	}
}
