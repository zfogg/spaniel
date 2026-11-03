package dashboards

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/servicecolor"
)

func TestTrustedGaugeSanitizesAdmittedNumericStrings(t *testing.T) {
	data := apigen.QueryPreview{Rows: []map[string]any{{"value": "\r50\t\n"}}}
	config := settings{Unit: "jobs", Thresholds: []any{map[string]any{"label": "review\x1b[2J", "value": "\n40\r\t"}}}
	for _, line := range renderGauge(config, data.Rows, 40) {
		if strings.ContainsAny(line, "\r\t\n\x1b\a") {
			t.Fatalf("trusted gauge leaked a control: %q", line)
		}
	}
	text := strings.Join(renderGauge(config, data.Rows, 40), "\n")
	if !strings.Contains(text, "50") || !strings.Contains(text, "40") || !strings.Contains(text, "reached") || strings.Count(text, "█") != 19 {
		t.Fatal("sanitization changed gauge meaning", text)
	}
	if data.Rows[0]["value"] != "\r50\t\n" {
		t.Fatal("presentation changed query data")
	}
}

func TestSemanticServiceColumnsMatchAtBothLayouts(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	const canonical = " 服务-🚀 canonical full name "
	c := servicecolor.Color(canonical).Text(true)
	color := fmt.Sprintf("38;2;%d;%d;%d", c.R, c.G, c.B)
	for _, column := range []string{"service.name", "service_name", "service"} {
		for _, width := range []int{24, 80} {
			text := strings.Join(layoutTable([]string{column}, [][]string{{canonical}}, width), "\n")
			if !strings.Contains(text, color) || !strings.Contains(ansi.Strip(text), "服务-🚀") {
				t.Fatal("semantic service color drifted", column, width, text)
			}
		}
	}
}
