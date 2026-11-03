// Package metadata renders compact identity and timing tables for inspectors.
package metadata

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type Field struct {
	Name, Value string
	Style       lipgloss.Style
	Service     bool // Value is a canonical service name, before display clipping.
}

// Table keeps each field on one physical row, including at narrow widths.
func Table(fields []Field, width int) []string {
	glyphs := terminal.Glyphs()
	width = max(1, width)
	keyWidth := 5
	for _, field := range fields {
		keyWidth = max(keyWidth, ansi.StringWidth(field.Name))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	cell := func(value string, size int) string {
		value = terminal.SingleLine(value)
		value = terminal.FitTimestamp(value, size)
		return value + strings.Repeat(" ", max(0, size-ansi.StringWidth(value)))
	}
	row := func(key, value string, keyStyle, valueStyle lipgloss.Style, service bool) string {
		if width < 5 {
			return tui.RenderContent(valueStyle, cell(key+" "+value, width))
		}
		valueCell := cell(value, width-keyWidth-3)
		if service {
			valueCell = tui.RenderServiceName(value, valueCell)
		} else {
			valueCell = tui.RenderContent(valueStyle, valueCell)
		}
		return tui.RenderContent(keyStyle, cell(key, keyWidth)) + tui.RenderContent(tui.ContentRule, " "+glyphs.Pipe+" ") + valueCell
	}
	lines := []string{row("FIELD", "VALUE", tui.ContentHeading, tui.ContentHeading, false), tui.RenderContent(tui.ContentRule, strings.Repeat(glyphs.Rule, width))}
	for _, field := range fields {
		lines = append(lines, row(field.Name, field.Value, tui.ContentLabel, field.Style.Inherit(tui.ContentValue), field.Service))
	}
	return lines
}
