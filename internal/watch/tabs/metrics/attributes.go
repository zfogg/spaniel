package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type attributeCell struct{ key, kind, value string }

// attributeTable keeps typed leaf values separate from paths and expands
// containers instead of hiding arbitrary telemetry in a serialized JSON cell.
func attributeTable(title string, attributes map[string]any, width int) []string {
	lines := []string{title}
	if len(attributes) == 0 {
		return append(lines, "(none)")
	}
	rows := []attributeCell{}
	var visit func(string, any)
	visit = func(path string, value any) {
		switch v := value.(type) {
		case map[string]any:
			rows = append(rows, attributeCell{path, "object", fmt.Sprintf("%d fields", len(v))})
			names := make([]string, 0, len(v))
			for name := range v {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				visit(path+"["+strconv.Quote(name)+"]", v[name])
			}
		case []any:
			rows = append(rows, attributeCell{path, "array", fmt.Sprintf("%d items", len(v))})
			for i, item := range v {
				visit(fmt.Sprintf("%s[%d]", path, i), item)
			}
		case string:
			text := strconv.Quote(v)
			rows = append(rows, attributeCell{path, "string", text[1 : len(text)-1]})
		case json.Number:
			kind := "integer"
			if strings.ContainsAny(v.String(), ".eE") {
				kind = "number"
			}
			rows = append(rows, attributeCell{path, kind, v.String()})
		case bool:
			rows = append(rows, attributeCell{path, "boolean", strconv.FormatBool(v)})
		case nil:
			rows = append(rows, attributeCell{path, "null", "null"})
		case float64:
			rows = append(rows, attributeCell{path, "number", strconv.FormatFloat(v, 'g', -1, 64)})
		case float32:
			rows = append(rows, attributeCell{path, "number", strconv.FormatFloat(float64(v), 'g', -1, 32)})
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			rows = append(rows, attributeCell{path, "integer", fmt.Sprint(v)})
		default:
			rows = append(rows, attributeCell{path, fmt.Sprintf("%T", v), fmt.Sprint(v)})
		}
	}
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(name, attributes[name])
	}
	if width < 24 {
		lines = append(lines, "KEY / TYPE / VALUE")
		for _, row := range rows {
			lines = append(lines, strings.Split(ansi.Hardwrap(safe(row.key)+" / "+row.kind, max(1, width), true), "\n")...)
			lines = append(lines, strings.Split(ansi.Hardwrap(safe(row.value), max(1, width), true), "\n")...)
		}
		return lines
	}
	keyWidth := min(32, max(6, width/3))
	typeWidth := 7
	valueWidth := max(1, width-keyWidth-typeWidth-6)
	appendRow := func(keyText, typeText, valueText string) {
		cells := [][]string{strings.Split(ansi.Hardwrap(safe(keyText), keyWidth, true), "\n"), strings.Split(ansi.Hardwrap(safe(typeText), typeWidth, true), "\n"), strings.Split(ansi.Hardwrap(safe(valueText), valueWidth, true), "\n")}
		for i := 0; i < max(len(cells[0]), len(cells[1]), len(cells[2])); i++ {
			parts := []string{"", "", ""}
			for j := range cells {
				if i < len(cells[j]) {
					parts[j] = cells[j][i]
				}
			}
			parts[0] += strings.Repeat(" ", max(0, keyWidth-ansi.StringWidth(parts[0])))
			parts[1] += strings.Repeat(" ", max(0, typeWidth-ansi.StringWidth(parts[1])))
			lines = append(lines, strings.Join(parts, " | "))
		}
	}
	appendRow("KEY", "TYPE", "VALUE")
	lines = append(lines, strings.Repeat("─", keyWidth)+"─┼─"+strings.Repeat("─", typeWidth)+"─┼─"+strings.Repeat("─", valueWidth))
	for _, row := range rows {
		appendRow(row.key, row.kind, row.value)
	}
	return lines
}
