package traces

import (
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func attributeLines(title, raw string, width int) []string {
	lines := []string{"", tui.RenderContent(tui.ContentTitle, title+" · key/value table")}
	rows, err := attributes.Parse(raw)
	if err != nil {
		return append(append(lines, "Invalid attributes JSON; raw value follows:"), terminal.Wrap(raw, width)...)
	}
	if len(rows) == 0 {
		return append(lines, "(none)")
	}
	return append(lines, attributes.Render(rows, width)...)
}
