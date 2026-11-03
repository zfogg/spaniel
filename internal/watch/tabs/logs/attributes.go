package logs

import (
	"github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/terminal"
	"strings"
)

func attributeTable(raw string, width int) string {
	rows, err := attributes.Parse(raw)
	if err != nil {
		return "Invalid attribute JSON · retained raw value\n" + strings.Join(terminal.Wrap(raw, width), "\n")
	}
	if len(rows) == 0 {
		return "(none)"
	}
	return strings.Join(attributes.Render(rows, width), "\n")
}
