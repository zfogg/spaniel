package logs

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type attribute struct{ path, kind, value string }

// UseNumber keeps attribute integers/decimals exact. These are telemetry values,
// not timestamps to reformat. Expand containers into typed, navigable paths so
// nested values remain readable without a serialized JSON blob.
func attributeTable(raw string, width int) string {
	if strings.TrimSpace(raw) == "" {
		return "(none)"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "Invalid attribute JSON · retained raw value\n" + clean(raw)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "Invalid attribute JSON · retained raw value\n" + clean(raw)
	}
	var rows []attribute
	var walk func(string, any)
	walk = func(path string, value any) {
		switch value := value.(type) {
		case map[string]any:
			if path != "" {
				rows = append(rows, attribute{path, "object", fmt.Sprintf("%d keys", len(value))})
			}
			keys := make([]string, 0, len(value))
			for name := range value {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			for _, name := range keys {
				child := path + "[" + strconv.Quote(name) + "]"
				if path == "" {
					child = name
					if strings.ContainsAny(name, "[]\"") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
						child = strconv.Quote(name)
					}
				}
				walk(child, value[name])
			}
		case []any:
			if path == "" {
				path = "(root)"
			}
			rows = append(rows, attribute{path, "array", fmt.Sprintf("%d items", len(value))})
			for i, child := range value {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		case json.Number:
			rows = append(rows, attribute{rootPath(path), "number", value.String()})
		case string:
			if value == "" {
				value = `""`
			}
			rows = append(rows, attribute{rootPath(path), "string", clean(value)})
		case bool:
			rows = append(rows, attribute{rootPath(path), "boolean", strconv.FormatBool(value)})
		case nil:
			rows = append(rows, attribute{rootPath(path), "null", "null"})
		}
	}
	walk("", value)
	if len(rows) == 0 {
		return "(none)"
	}
	width = max(1, width)
	if width < 32 {
		// Stack typed cells in a narrow terminal; no key or value is truncated.
		lines := []string{"KEY [TYPE] / VALUE"}
		for _, row := range rows {
			lines = append(lines, row.path+" ["+row.kind+"]", row.value, "")
		}
		return ansi.Hardwrap(strings.Join(lines, "\n"), width, true)
	}
	keyWidth, typeWidth := min(36, width/3), 7
	valueWidth := width - keyWidth - typeWidth - 6
	var lines []string
	writeRow := func(key, kind, value string) {
		columns := [][]string{
			strings.Split(ansi.Hardwrap(clean(key), keyWidth, true), "\n"),
			strings.Split(ansi.Hardwrap(kind, typeWidth, true), "\n"),
			strings.Split(ansi.Hardwrap(value, valueWidth, true), "\n"),
		}
		for n := 0; n < max(len(columns[0]), len(columns[1]), len(columns[2])); n++ {
			fields := [3]string{}
			for i := range fields {
				if n < len(columns[i]) {
					fields[i] = columns[i][n]
				}
			}
			lines = append(lines, cell(fields[0], keyWidth)+" │ "+cell(fields[1], typeWidth)+" │ "+cell(fields[2], valueWidth))
		}
	}
	writeRow("KEY", "TYPE", "VALUE")
	lines = append(lines, strings.Repeat("─", width))
	for _, row := range rows {
		writeRow(row.path, row.kind, row.value)
	}
	return strings.Join(lines, "\n")
}

func rootPath(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}
