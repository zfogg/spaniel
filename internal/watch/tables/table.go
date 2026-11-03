// Package tables configures Charmbracelet's table renderer for watch views.
package tables

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

// Render delegates alignment, padding, row heights, wrapping, and border joins
// to Lipgloss. Callers supply safe cell values, content widths and semantic styles.
func Render(headers []string, rows [][]string, widths []int, wrap, dividers, headerRule bool, style table.StyleFunc) []string {
	if len(widths) == 0 {
		return nil
	}
	border := lipgloss.NormalBorder()
	if terminal.Glyphs().Pipe == "|" {
		border.Left, border.Right, border.Top, border.Bottom = "|", "|", "-", "-"
		border.Middle, border.MiddleLeft, border.MiddleRight = "+", "+", "+"
	}
	borderStyle := tui.ContentRule
	if !tui.ContentEnabled() {
		borderStyle = lipgloss.NewStyle()
	}
	total := 0
	for _, width := range widths {
		total += width
	}
	gap := 2
	if dividers {
		gap = 3
	}
	total += gap * (len(widths) - 1)
	t := table.New().Border(border).BorderStyle(borderStyle).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(dividers).BorderHeader(headerRule).Width(total).Wrap(wrap).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle()
			if style != nil && tui.ContentEnabled() {
				s = style(row, col)
			}
			left, right := 0, 0
			if dividers {
				if col > 0 {
					left = 1
				}
				if col < len(widths)-1 {
					right = 1
				}
			} else if col < len(widths)-1 {
				right = 2
			}
			return s.UnderlineSpaces(false).Width(widths[col] + left + right).PaddingLeft(left).PaddingRight(right)
		})
	if len(headers) > 0 {
		t.Headers(headers...)
	}
	t.Rows(rows...)
	view := t.Render()
	if !tui.ContentEnabled() {
		view = ansi.Strip(view)
	}
	return strings.Split(view, "\n")
}
