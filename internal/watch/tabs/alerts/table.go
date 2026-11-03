package alerts

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/tables"
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
	headers := make([]string, len(columns))
	cells := make([][]string, len(rows))
	for i, column := range columns {
		headers[i] = terminal.FitTimestamp(oneLine(column.title), widths[i])
	}
	for i, row := range rows {
		cells[i] = make([]string, len(columns))
		for j, column := range columns {
			text := ""
			if j < len(row) {
				text = row[j]
			}
			cells[i][j] = terminal.FitTimestamp(oneLine(text), widths[j])
			if column.state {
				cells[i][j] = tone(cells[i][j])
			}
		}
	}
	lines := tables.Render(headers, cells, widths, false, true, false, func(row, col int) lipgloss.Style {
		if row < 0 {
			return tui.ContentHeading
		}
		return tui.ContentValue
	})
	for i := range lines {
		if i == 0 {
			lines[i] = "  " + lines[i]
			continue
		}
		index := i - 1
		isSelected := index < len(ids) && ids[index] == selected
		lines[i] = tui.ContentRow(marker(isSelected)+lines[i], isSelected)
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
