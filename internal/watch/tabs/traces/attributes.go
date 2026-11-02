package traces

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
)

type attributeRow struct{ key, kind, value string }

// UseNumber is essential: attribute IDs/counters can exceed both JavaScript's
// integer precision and uint64. No attribute number passes through float64.
func parseAttributes(raw string) ([]attributeRow, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing attribute data")
	}
	type node struct {
		key   string
		value any
		depth int
	}
	var stack []node
	pushObject := func(object map[string]any, depth int) {
		keys := make([]string, 0, len(object))
		for k := range object {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i := len(keys) - 1; i >= 0; i-- {
			stack = append(stack, node{key: keys[i], value: object[keys[i]], depth: depth})
		}
	}
	if object, ok := value.(map[string]any); ok {
		pushObject(object, 0)
	} else {
		stack = append(stack, node{key: "(value)", value: value})
	}
	var rows []attributeRow
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		label := clean(n.key)
		if label == "" {
			label = `""`
		}
		if n.depth > 0 {
			label = strings.Repeat("  ", min(n.depth, 6)) + "↳ " + label
			if n.depth > 6 {
				label = fmt.Sprintf("[depth %d] %s", n.depth, label)
			}
		}
		row := attributeRow{key: label}
		switch v := n.value.(type) {
		case nil:
			row.kind, row.value = "null", "null"
		case string:
			row.kind, row.value = "string", v
			if v == "" {
				row.value = "(empty string)"
			}
		case bool:
			row.kind, row.value = "boolean", strconv.FormatBool(v)
		case json.Number:
			row.kind, row.value = "number", v.String()
		case map[string]any:
			row.kind, row.value = "object", fmt.Sprintf("%d fields", len(v))
			pushObject(v, n.depth+1)
		case []any:
			row.kind, row.value = "array", fmt.Sprintf("%d items", len(v))
			for i := len(v) - 1; i >= 0; i-- {
				stack = append(stack, node{key: fmt.Sprintf("[%d]", i), value: v[i], depth: n.depth + 1})
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func wrapCell(value string, width int) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		out = append(out, strings.Split(ansi.Hardwrap(clean(line), max(1, width), true), "\n")...)
	}
	return out
}

func attributeLines(title, raw string, width int) []string {
	lines := []string{"", tui.Title.Render(title + " · key/value table")}
	rows, err := parseAttributes(raw)
	if err != nil {
		lines = append(lines, "Invalid attributes JSON; raw value follows:")
		return append(lines, wrapCell(raw, width)...)
	}
	if len(rows) == 0 {
		return append(lines, "(none)")
	}
	if width < 24 {
		// A stacked table keeps every key, type and value accessible when
		// there is insufficient room for useful side-by-side columns.
		for _, row := range rows {
			lines = append(lines, strings.Repeat("─", max(1, width)))
			lines = append(lines, wrapCell("Key: "+row.key, width)...)
			lines = append(lines, wrapCell("Type: "+row.kind, width)...)
			lines = append(lines, wrapCell("Value: "+row.value, width)...)
		}
		return lines
	}
	keyWidth := min(36, max(8, width/3))
	typeWidth := 7
	if width < 56 {
		typeWidth = 0
	}
	valueWidth := width - keyWidth - 3
	header := pad("KEY", keyWidth) + " │ " + "VALUE (TYPE)"
	divider := strings.Repeat("─", keyWidth) + "─┼─" + strings.Repeat("─", valueWidth)
	if typeWidth > 0 {
		valueWidth -= typeWidth + 3
		header = pad("KEY", keyWidth) + " │ " + pad("TYPE", typeWidth) + " │ VALUE"
		divider = strings.Repeat("─", keyWidth) + "─┼─" + strings.Repeat("─", typeWidth) + "─┼─" + strings.Repeat("─", valueWidth)
	}
	lines = append(lines, tui.Muted.Render(header), tui.Muted.Render(divider))
	for _, row := range rows {
		value := row.value
		if typeWidth == 0 {
			value = "[" + row.kind + "]\n" + value
		}
		keys, values := wrapCell(row.key, keyWidth), wrapCell(value, valueWidth)
		for i := 0; i < max(len(keys), len(values)); i++ {
			k, v, kind := "", "", ""
			if i < len(keys) {
				k = keys[i]
			}
			if i < len(values) {
				v = values[i]
			}
			if i == 0 {
				kind = row.kind
			}
			line := pad(k, keyWidth) + " │ "
			if typeWidth > 0 {
				line += pad(kind, typeWidth) + " │ "
			}
			lines = append(lines, line+v)
		}
	}
	return lines
}
