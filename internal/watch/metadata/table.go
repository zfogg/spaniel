// Package metadata renders compact identity and timing tables for inspectors.
package metadata

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/tables"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type Field struct {
	Name, Value string
	Style       lipgloss.Style
	Service     bool // Value is a canonical service name, before display clipping.
}

// Table keeps identity/timing values on one row; Lipgloss aligns the cells.
func Table(fields []Field, width int) []string {
	width = max(1, width)
	keyWidth := 5
	for _, field := range fields {
		keyWidth = max(keyWidth, ansi.StringWidth(field.Name))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	fit := func(value string, size int) string { return terminal.FitTimestamp(terminal.SingleLine(value), size) }
	if width < 5 {
		rows := make([][]string, 0, len(fields))
		for _, field := range fields {
			rows = append(rows, []string{fit(field.Name+" "+field.Value, width)})
		}
		return tables.Render([]string{fit("FIELD VALUE", width)}, rows, []int{width}, false, false, true, func(row, col int) lipgloss.Style {
			if row < 0 {
				return tui.ContentHeading
			}
			return fields[row].Style.Inherit(tui.ContentValue)
		})
	}
	widths := []int{keyWidth, max(1, width-keyWidth-3)}
	rows := make([][]string, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, []string{fit(field.Name, widths[0]), fit(field.Value, widths[1])})
	}
	return tables.Render([]string{fit("FIELD", widths[0]), fit("VALUE", widths[1])}, rows, widths, false, true, true, func(row, col int) lipgloss.Style {
		if row < 0 {
			return tui.ContentHeading
		}
		if col == 0 {
			return tui.ContentLabel
		}
		if fields[row].Service {
			return tui.ServiceNameStyle(fields[row].Value)
		}
		return fields[row].Style.Inherit(tui.ContentValue)
	})
}
