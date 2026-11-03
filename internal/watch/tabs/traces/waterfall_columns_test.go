package traces

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func TestWaterfallHeadersShareRowCellStarts(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("SPANIEL_ASCII", "0")
	span := flameSpan("span", "", "服务-marker", "operation-marker界", 1000, 2000)
	span.StatusCode = 2
	for _, mode := range []struct {
		profile termenv.Profile
		plain   string
	}{{termenv.TrueColor, ""}, {termenv.ANSI256, ""}, {termenv.Ascii, ""}, {termenv.TrueColor, "1"}} {
		lipgloss.SetColorProfile(mode.profile)
		t.Setenv("NO_COLOR", mode.plain)
		for _, width := range []int{40, 60, 62, 80, 120} {
			for _, selected := range []bool{false, true} {
				header := ansi.Strip(waterfallHeader(width, 1000, 2000))
				row := ansi.Strip(waterfallRow(flatSpan{span: span}, selected, 1000, 2000, width))
				pairs := map[string]string{"OPERATION": "operation-marker", "DURATION": duration(span.DurationNs), "STATUS": "ERROR"}
				if width >= 62 {
					pairs["Waterfall"] = terminal.Glyphs().Pipe
				}
				for heading, value := range pairs {
					h, v := strings.Index(header, heading), strings.Index(row, value)
					if h < 0 || v < 0 || ansi.StringWidth(header[:h]) != ansi.StringWidth(row[:v]) {
						t.Fatalf("width%d selected%t: %s misaligned\n%s\n%s", width, selected, heading, header, row)
					}
				}
				if ansi.StringWidth(row) > width || ansi.StringWidth(header) > width {
					t.Fatal("waterfall overflow", width, row)
				}
			}
		}
	}
}
