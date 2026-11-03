package metrics

import (
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func attributeTable(title string, values map[string]any, width int) []string {
	lines := terminal.Wrap(title, width)
	for i := range lines {
		lines[i] = tui.RenderContent(tui.ContentTitle, lines[i])
	}
	rows := attributes.Rows(values)
	if len(rows) == 0 {
		for _, line := range terminal.Wrap("(none)", width) {
			lines = append(lines, tui.RenderContent(tui.ContentContext, line))
		}
		return lines
	}
	return append(lines, attributes.Render(rows, width)...)
}
