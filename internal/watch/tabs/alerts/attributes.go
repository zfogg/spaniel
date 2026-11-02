package alerts

import (
	"fmt"
	"github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/terminal"
	"time"
)

func (m *model) attributeTable(raw string) []string {
	rows, err := attributes.Parse(raw)
	if err != nil {
		return append([]string{"Invalid attribute map: " + oneLine(err.Error())}, terminal.Wrap(raw, max(1, m.width-1))...)
	}
	if len(rows) == 0 {
		return []string{"(no attributes)"}
	}
	return attributes.Render(rows, max(1, m.width-1))
}

func duration(ns int64) string {
	if ns == 0 {
		return "0s"
	}
	d := time.Duration(ns)
	magnitude := d
	if magnitude < 0 {
		magnitude = -magnitude
	}
	switch {
	case magnitude < time.Millisecond:
		return fmt.Sprintf("%.3gµs", float64(ns)/1e3)
	case magnitude < time.Second:
		return fmt.Sprintf("%.3gms", float64(ns)/1e6)
	case magnitude < time.Minute:
		return fmt.Sprintf("%.3gs", float64(ns)/1e9)
	default:
		return d.Round(time.Millisecond).String()
	}
}
