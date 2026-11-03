// Package attributes parses and lays out complete, typed telemetry values.
// Identity/timing fields use metadata's separate single-row ellipsis policy.
package attributes

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type Row struct{ Key, Type, Value string }

// Parse never routes JSON numbers through floating point and rejects trailing
// data. Empty input and an empty root object represent an absent attribute map.
func Parse(raw string) ([]Row, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON value; invalid trailing data")
	}
	return Rows(value), nil
}

// Rows also accepts native typed maps/slices and integers, without a lossy
// JSON round trip. Nested paths use quoted brackets, so literal keys containing
// brackets, quotes, control characters or the root sentinel remain distinct.
func Rows(value any) []Row {
	var rows []Row
	var visit func(string, any)
	visit = func(path string, value any) {
		if number, ok := value.(json.Number); ok {
			rows = append(rows, Row{path, "number", number.String()})
			return
		}
		v := reflect.ValueOf(value)
		if !v.IsValid() {
			rows = append(rows, Row{path, "null", "null"})
			return
		}
		switch v.Kind() {
		case reflect.Map:
			if v.Type().Key().Kind() != reflect.String {
				break
			}
			if path != "" {
				rows = append(rows, Row{path, "object", fmt.Sprintf("%d fields", v.Len())})
			}
			keys := v.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, key := range keys {
				name := key.String()
				child := path + "[" + strconv.Quote(name) + "]"
				if path == "" {
					child = rootKey(name)
				}
				visit(child, v.MapIndex(key).Interface())
			}
			return
		case reflect.Array, reflect.Slice:
			rows = append(rows, Row{path, "array", fmt.Sprintf("%d items", v.Len())})
			for i := 0; i < v.Len(); i++ {
				visit(fmt.Sprintf("%s[%d]", path, i), v.Index(i).Interface())
			}
			return
		case reflect.String:
			text := v.String()
			if text == "" {
				text = "(empty string)"
			}
			rows = append(rows, Row{path, "string", text})
			return
		case reflect.Bool:
			rows = append(rows, Row{path, "boolean", strconv.FormatBool(v.Bool())})
			return
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			rows = append(rows, Row{path, "integer", strconv.FormatInt(v.Int(), 10)})
			return
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			rows = append(rows, Row{path, "integer", strconv.FormatUint(v.Uint(), 10)})
			return
		case reflect.Float32, reflect.Float64:
			rows = append(rows, Row{path, "number", strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())})
			return
		}
		rows = append(rows, Row{path, fmt.Sprintf("%T", value), fmt.Sprint(value)})
	}
	root := reflect.ValueOf(value)
	if root.IsValid() && root.Kind() == reflect.Map && root.Type().Key().Kind() == reflect.String {
		visit("", value)
	} else {
		visit("(root)", value)
	}
	return rows
}

func rootKey(key string) string {
	if key == "" || key == "(root)" || strings.ContainsAny(key, "[]\"\\") || strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '\u2028' || r == '\u2029'
	}) >= 0 {
		return "[" + strconv.Quote(key) + "]"
	}
	return key
}

// Render wraps every cell before the owning inspector scrolls it. Wide views
// use aligned columns; narrow views stack labeled cells without truncation.
func Render(rows []Row, width int) []string {
	if len(rows) == 0 {
		return nil
	}
	width = max(1, width)
	var lines []string
	if width < 40 {
		for _, line := range terminal.Wrap("KEY / TYPE / VALUE", width) {
			lines = append(lines, tui.RenderContent(tui.ContentHeading, line))
		}
		for _, row := range rows {
			for _, line := range terminal.Wrap("Key: "+row.Key, width) {
				lines = append(lines, tui.RenderContent(tui.ContentLabel, line))
			}
			for _, line := range terminal.Wrap("Type: "+row.Type, width) {
				lines = append(lines, tui.RenderContent(tui.ContentContext, line))
			}
			for _, line := range terminal.Wrap("Value: "+row.Value, width) {
				value := tui.RenderContent(tui.ContentValue, line)
				if serviceAttribute(row) {
					value = tui.RenderServiceName(row.Value, line)
				}
				lines = append(lines, value)
			}
		}
		return lines
	}
	keyWidth := 8
	for _, row := range rows {
		keyWidth = max(keyWidth, ansi.StringWidth(terminal.SingleLine(row.Key)))
	}
	keyWidth = min(keyWidth, min(36, width/3))
	const typeWidth = 7
	valueWidth := width - keyWidth - typeWidth - 6
	pad := func(value string, size int) string {
		return value + strings.Repeat(" ", max(0, size-ansi.StringWidth(value)))
	}
	glyphs := terminal.Glyphs()
	separator, joint := " "+glyphs.Pipe+" ", glyphs.Rule+glyphs.Cross+glyphs.Rule
	styledSeparator := tui.RenderContent(tui.ContentRule, separator)
	lines = append(lines, tui.RenderContent(tui.ContentHeading, pad("KEY", keyWidth))+styledSeparator+tui.RenderContent(tui.ContentHeading, pad("TYPE", typeWidth))+styledSeparator+tui.RenderContent(tui.ContentHeading, "VALUE"))
	lines = append(lines, tui.RenderContent(tui.ContentRule, strings.Repeat(glyphs.Rule, keyWidth)+joint+strings.Repeat(glyphs.Rule, typeWidth)+joint+strings.Repeat(glyphs.Rule, valueWidth)))
	for _, row := range rows {
		cells := [3][]string{terminal.Wrap(row.Key, keyWidth), terminal.Wrap(row.Type, typeWidth), terminal.Wrap(row.Value, valueWidth)}
		for i := 0; i < max(len(cells[0]), len(cells[1]), len(cells[2])); i++ {
			var parts [3]string
			for column := range cells {
				if i < len(cells[column]) {
					parts[column] = cells[column][i]
				}
			}
			value := tui.RenderContent(tui.ContentValue, parts[2])
			if serviceAttribute(row) {
				value = tui.RenderServiceName(row.Value, parts[2])
			}
			lines = append(lines, tui.RenderContent(tui.ContentLabel, pad(parts[0], keyWidth))+styledSeparator+tui.RenderContent(tui.ContentContext, pad(parts[1], typeWidth))+styledSeparator+value)
		}
	}
	return lines
}

func serviceAttribute(row Row) bool {
	return row.Type == "string" && row.Value != "(empty string)" && (row.Key == "service.name" || row.Key == "service_name" || row.Key == "service" || row.Key == `service["name"]`)
}
