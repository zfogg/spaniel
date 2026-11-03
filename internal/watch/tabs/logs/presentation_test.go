package logs

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestStyledLogsPreservePayloadSelectionAndWidths(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, severity := range []float64{5, 9, 13, 17, 21} {
		row := logRow("request | payload • exact 9007199254740993\x1b]0;bad\a", 100)
		row.ServiceName = "服务-🚀-very-long-service-name"
		row.Severity = severity
		for _, width := range []int{1, 12, 40, 80, 120, 200} {
			for _, selected := range []bool{false, true} {
				styled := RenderRow(row, selected, width, time.Unix(1, 0))
				t.Setenv("NO_COLOR", "1")
				plain := RenderRow(row, selected, width, time.Unix(1, 0))
				t.Setenv("NO_COLOR", "")
				if ansi.Strip(styled) != plain || strings.Contains(plain, "\x1b") || ansi.StringWidth(styled) > width || strings.ContainsAny(plain, "\n\r\a") {
					t.Fatalf("severity %g, width %d, selected %t: %q / %q", severity, width, selected, styled, plain)
				}
				if width == 200 && !strings.Contains(plain, "9007199254740993") {
					t.Fatal("payload changed", plain)
				}
			}
		}
	}
}
