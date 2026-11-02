package alerts

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type attributeRow struct{ key, kind, value string }

func attributeRows(raw string) ([]attributeRow, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var attributes map[string]any
	if err := decoder.Decode(&attributes); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON object")
	}
	var rows []attributeRow
	var appendValue func(string, any)
	appendValue = func(path string, value any) {
		switch value := value.(type) {
		case map[string]any:
			if len(value) == 0 {
				rows = append(rows, attributeRow{path, "object", "(empty)"})
				return
			}
			var keys []string
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				encoded, _ := json.Marshal(key)
				appendValue(path+"["+string(encoded)+"]", value[key])
			}
		case []any:
			if len(value) == 0 {
				rows = append(rows, attributeRow{path, "array", "(empty)"})
				return
			}
			for i, element := range value {
				appendValue(fmt.Sprintf("%s[%d]", path, i), element)
			}
		case json.Number:
			rows = append(rows, attributeRow{path, "number", value.String()})
		case string:
			rows = append(rows, attributeRow{path, "string", value})
		case bool:
			rows = append(rows, attributeRow{path, "boolean", fmt.Sprint(value)})
		case nil:
			rows = append(rows, attributeRow{path, "null", "null"})
		}
	}
	var keys []string
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		appendValue(key, attributes[key])
	}
	return rows, nil
}

// attributeTable keeps exact JSON numbers as text. Nested fields and array
// indices become explicit key paths, with each value's type retained. Wrapping
// happens before the inspector's scrolling, so long keys and values stay whole.
func (m *model) attributeTable(raw string) []string {
	rows, err := attributeRows(raw)
	if err != nil {
		return append([]string{"Invalid attribute map: " + oneLine(err.Error())}, m.wrap(raw)...)
	}
	if len(rows) == 0 {
		return []string{"(no attributes)"}
	}
	width := max(1, m.width-1)
	var lines []string
	if width < 40 {
		for _, row := range rows {
			lines = append(lines, m.wrap("Key: "+row.key)...)
			lines = append(lines, "Type: "+row.kind)
			lines = append(lines, m.wrap("Value: "+row.value)...)
		}
		return lines
	}
	keyWidth := 8
	for _, row := range rows {
		keyWidth = max(keyWidth, ansi.StringWidth(clean(row.key)))
	}
	keyWidth = min(keyWidth, max(8, width/3))
	valueWidth := max(1, width-keyWidth-13)
	pad := func(text string, width int) string {
		return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
	}
	lines = append(lines, pad("KEY", keyWidth)+" | "+pad("TYPE", 7)+" | VALUE")
	for _, row := range rows {
		keys := strings.Split(ansi.Hardwrap(clean(row.key), keyWidth, true), "\n")
		values := strings.Split(ansi.Hardwrap(clean(row.value), valueWidth, true), "\n")
		for i := 0; i < max(len(keys), len(values)); i++ {
			key, value, kind := "", "", ""
			if i < len(keys) {
				key = keys[i]
			}
			if i < len(values) {
				value = values[i]
			}
			if i == 0 {
				kind = row.kind
			}
			lines = append(lines, pad(key, keyWidth)+" | "+pad(kind, 7)+" | "+value)
		}
	}
	return lines
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
