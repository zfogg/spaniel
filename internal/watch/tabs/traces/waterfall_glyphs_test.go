package traces

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWaterfallEdgesUseTerminalGlyphsWithoutChangingPayloadOrWidth(t *testing.T) {
	widths := map[int]int{}
	for _, mode := range []struct{ ascii, edge string }{{"0", "│"}, {"1", "|"}} {
		t.Run("ascii="+mode.ascii, func(t *testing.T) {
			t.Setenv("SPANIEL_ASCII", mode.ascii)
			span := sampleSpan("payload | │", "", 1000)
			for _, width := range []int{62, 80, 120} {
				line := ansi.Strip(waterfallRow(flatSpan{span: span}, false, 1000, 1100, width))
				bar := strings.IndexRune(line, '█')
				if bar < 0 || !strings.HasSuffix(line[:bar], mode.edge) || !strings.HasSuffix(line, mode.edge) {
					t.Fatalf("mode %s width %d: wrong bar edges: %q", mode.ascii, width, line)
				}
				if !strings.Contains(line, "payload | │") {
					t.Fatal("UI glyph policy changed telemetry text", line)
				}
				columns := ansi.StringWidth(line)
				if columns > width || widths[width] != 0 && widths[width] != columns {
					t.Fatal("glyph mode changed waterfall geometry", width, columns, widths[width])
				}
				widths[width] = columns
			}
		})
	}
}
