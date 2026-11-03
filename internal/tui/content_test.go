package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/servicecolor"
)

func TestContentCapabilitiesAndCanonicalService(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	const canonical = " 服务-🚀 with full name "
	const display = "服务…"
	for _, depth := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(depth)
		for _, background := range []bool{false, true} {
			lipgloss.SetHasDarkBackground(background)
			got := RenderServiceName(canonical, display)
			if ansi.Strip(got) != display || ansi.StringWidth(got) != ansi.StringWidth(display) {
				t.Fatal("service styling changed laid-out text", got)
			}
			if depth == termenv.TrueColor {
				color := servicecolor.Color(canonical).Text(background)
				if !strings.Contains(got, fmt.Sprintf("38;2;%d;%d;%d", color.R, color.G, color.B)) {
					t.Fatal("service was hashed after clipping", got)
				}
			} else if strings.Contains(got, "38;2;") {
				t.Fatal("forced truecolor on a lower-depth terminal", got)
			}
			if depth == termenv.Ascii && got != display {
				t.Fatal("plain terminal received attributes", got)
			}
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, mode := range []string{"NO_COLOR", "dumb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "NO_COLOR" {
				t.Setenv("NO_COLOR", "1")
			} else {
				t.Setenv("TERM", "dumb")
			}
			if RenderServiceName(canonical, display) != display || RenderContent(ContentHeading, "FIELD") != "FIELD" || RenderContent(ContentTitle, "\x1b[1mTitle\x1b[m") != "Title" {
				t.Fatal("plain mode retained ANSI")
			}
		})
	}
}

func TestSeverityRolesRemainDistinctAndReadable(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	seen := map[string]bool{}
	for _, severity := range []int{5, 9, 13, 17, 21} {
		styled := RenderContent(ContentSeverityStyle(severity), "LEVEL")
		if seen[styled] || ansi.Strip(styled) != "LEVEL" {
			t.Fatal("severity lost its distinct role", severity, styled)
		}
		seen[styled] = true
	}
}

func TestSelectedRowRestoresBackgroundAfterNestedStyles(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	level := RenderContent(ContentSeverityStyle(21), "FATAL")
	service := RenderServiceName("api", "api")
	text := "› " + level + " " + service + " payload"
	selected := ContentRow(text, true)
	if ansi.Strip(selected) != "› FATAL api payload" || !strings.Contains(selected, level) || !strings.Contains(selected, service) {
		t.Fatal("selection changed semantic content", selected)
	}
	sample := ContentSelected.Render(" ")
	start := sample[:strings.IndexByte(sample, ' ')]
	if strings.Count(selected, start) != strings.Count(text, "\x1b[0m")+1 {
		t.Fatal("nested resets lost the selected background", selected)
	}
}

func TestErrorRedAndFatalPurple(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	for _, mode := range []struct {
		dark bool
		red  string
	}{{false, "38;2;185;28;28"}, {true, "38;2;248;113;113"}} {
		lipgloss.SetHasDarkBackground(mode.dark)
		if got := RenderContent(ContentSeverityStyle(17), "ERROR"); !strings.Contains(got, mode.red) {
			t.Fatal("ERROR is not red", got)
		}
		if got := RenderContent(ContentSeverityStyle(21), "FATAL"); !strings.Contains(got, "48;2;126;34;206") || !strings.Contains(got, "38;2;255;255;255") {
			t.Fatal("FATAL lost its purple badge contrast", got)
		}
	}
}
