// Package metadata renders compact identity and timing tables for inspectors.
package metadata

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
)

type Field struct {
	Name, Value string
	Style       lipgloss.Style
}

// Table keeps each field on one physical row, including at narrow widths.
func Table(fields []Field, width int) []string {
	width = max(1, width)
	keyWidth := 5
	for _, field := range fields {
		keyWidth = max(keyWidth, ansi.StringWidth(field.Name))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	cell := func(value string, size int) string {
		value = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
				return ' '
			}
			return r
		}, ansi.Strip(value))
		value = ansi.Truncate(value, size, "…")
		return value + strings.Repeat(" ", max(0, size-ansi.StringWidth(value)))
	}
	row := func(key, value string) string {
		if width < 5 {
			return cell(key+" "+value, width)
		}
		return cell(key, keyWidth) + " │ " + cell(value, width-keyWidth-3)
	}
	lines := []string{tui.Muted.Render(row("FIELD", "VALUE")), tui.Muted.Render(strings.Repeat("─", width))}
	for _, field := range fields {
		lines = append(lines, field.Style.Render(row(field.Name, field.Value)))
	}
	return lines
}
