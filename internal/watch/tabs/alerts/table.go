package alerts

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type tableColumn struct {
	title string
	width int
	state bool
}

// table renders bounded, aligned cells; a zero-width column receives remaining
// space. Callers omit lower-priority columns at narrow sizes. Full cell values
// remain available through the corresponding scrollable inspection.
func (m *model) table(columns []tableColumn, rows [][]string, ids []string, selected string) []string {
	remaining := max(1, m.width-2-3*(len(columns)-1))
	for _, column := range columns {
		remaining -= column.width
	}
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = column.width
		if widths[i] == 0 {
			widths[i] = max(1, remaining)
		}
	}
	cell := func(text string, width int, state bool) string {
		text = oneLine(text)
		truncated := terminal.FitTimestamp(text, width)
		padding := strings.Repeat(" ", max(0, width-ansi.StringWidth(truncated)))
		if state {
			truncated = tone(truncated)
		}
		return truncated + padding
	}
	var header []string
	for i, column := range columns {
		header = append(header, cell(column.title, widths[i], false))
	}
	separator := terminal.ColumnSeparator()
	lines := []string{"  " + strings.Join(header, separator)}
	for index, row := range rows {
		var cells []string
		for i, column := range columns {
			text := ""
			if i < len(row) {
				text = row[i]
			}
			cells = append(cells, cell(text, widths[i], column.state))
		}
		lines = append(lines, marker(index < len(ids) && ids[index] == selected)+strings.Join(cells, separator))
	}
	return lines
}

func (m *model) ruleLines(rowsIDs []string) []string {
	rules := m.visibleRules()
	columns := []tableColumn{{"NAME", 0, false}}
	if m.width >= 22 {
		columns = []tableColumn{{"STATE", 10, true}, {"NAME", 0, false}}
	}
	if m.width >= 40 {
		columns = []tableColumn{{"STATE", 12, true}, {"SEVERITY", 8, false}, {"NAME", 0, false}}
	}
	if m.width >= 65 {
		columns = append(columns, tableColumn{"INSTANCES", 9, false})
	}
	if m.width >= 95 {
		columns = append(columns, tableColumn{"LAST EVAL (UTC)", 20, false})
	}
	var rows [][]string
	for _, r := range rules {
		var values []string
		for _, column := range columns {
			switch column.title {
			case "STATE":
				values = append(values, ruleState(r))
			case "SEVERITY":
				values = append(values, string(r.Severity))
			case "NAME":
				values = append(values, r.Name)
			case "INSTANCES":
				values = append(values, strconv.Itoa(len(r.Instances)))
			case "LAST EVAL (UTC)":
				values = append(values, m.stamp(r.LastEvaluatedAt))
			}
		}
		rows = append(rows, values)
	}
	lines := m.table(columns, rows, rowsIDs, m.selected)
	if len(rules) == 0 && m.loads["list"].loaded {
		lines = append(lines, "No alert rules match this page.")
	}
	return m.selectedWindow(lines, m.selected, rowsIDs, 1)
}
