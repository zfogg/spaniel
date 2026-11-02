package metrics

import (
	"github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func attributeTable(title string, values map[string]any, width int) []string {
	lines := terminal.Wrap(title, width)
	rows := attributes.Rows(values)
	if len(rows) == 0 {
		return append(lines, terminal.Wrap("(none)", width)...)
	}
	return append(lines, attributes.Render(rows, width)...)
}
