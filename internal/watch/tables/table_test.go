package tables

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/tui"
)

func TestCharmTableAlignsWithoutUnderliningPadding(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SPANIEL_ASCII", "0")
	lines := Render([]string{"SERVICE NAME", "VALUE"}, [][]string{{"api", "9007199254740993"}}, []int{18, 20}, false, true, true, func(row, col int) lipgloss.Style {
		if row < 0 {
			return tui.ContentHeading
		}
		return tui.ContentValue
	})
	if len(lines) != 3 {
		t.Fatalf("unexpected table height: %q", lines)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != 41 {
			t.Fatalf("alignment width: %q", line)
		}
	}
	header := lines[0]
	underline, visibleUnderlines := false, 0
	for len(header) > 0 {
		if strings.HasPrefix(header, "\x1b[") {
			end := strings.IndexByte(header, 'm')
			if end < 0 {
				t.Fatal("incomplete SGR")
			}
			parts := strings.Split(header[2:end], ";")
			for i := 0; i < len(parts); i++ {
				n, _ := strconv.Atoi(parts[i])
				switch n {
				case 0:
					underline = false
				case 4:
					underline = true
				case 24:
					underline = false
				case 38, 48:
					if i+1 < len(parts) && parts[i+1] == "2" {
						i += 4
					} else if i+1 < len(parts) && parts[i+1] == "5" {
						i += 2
					}
				}
			}
			header = header[end+1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(header)
		header = header[size:]
		if r == ' ' && underline {
			t.Fatal("alignment/word spaces are underlined", lines[0])
		}
		if underline {
			visibleUnderlines++
		}
	}
	if visibleUnderlines == 0 {
		t.Fatal("visible header labels lost their underline")
	}
	if !strings.Contains(ansi.Strip(lines[2]), "9007199254740993") {
		t.Fatal("integer payload changed")
	}
}
