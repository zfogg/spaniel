package attributes

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func TestJSONPathsTypesAndExactNumbers(t *testing.T) {
	rows, err := Parse(`{"a":{"b":900719925474099312345},"a[\"b\"]":2,"":3,"(root)":4,"x\n":5,"array":[{"id":6}],"empty":{},"list":[],"decimal":1.234567890123456789,"string":"001","flag":true,"null":null}`)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Row{}
	for _, row := range rows {
		if _, exists := byKey[row.Key]; exists {
			t.Fatal("ambiguous attribute path", row.Key)
		}
		byKey[row.Key] = row
	}
	for key, want := range map[string]Row{
		`a["b"]`:       {Type: "number", Value: "900719925474099312345"},
		`["a[\"b\"]"]`: {Type: "number", Value: "2"},
		`[""]`:         {Type: "number", Value: "3"}, `["(root)"]`: {Type: "number", Value: "4"},
		`["x\n"]`: {Type: "number", Value: "5"}, `array[0]["id"]`: {Type: "number", Value: "6"},
		"empty": {Type: "object", Value: "0 fields"}, "list": {Type: "array", Value: "0 items"},
		"decimal": {Type: "number", Value: "1.234567890123456789"}, "string": {Type: "string", Value: "001"},
		"flag": {Type: "boolean", Value: "true"}, "null": {Type: "null", Value: "null"},
	} {
		want.Key = key
		if got := byKey[key]; got != want {
			t.Fatalf("%s: got %#v, want %#v", key, got, want)
		}
	}
	ordered, _ := Parse(`{"z":0,"a":1}`)
	if ordered[0].Key != "a" || ordered[1].Key != "z" {
		t.Fatal("object keys are not sorted", ordered)
	}
}

func TestAttributeGlyphModesPreservePayload(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Setenv("SPANIEL_ASCII", mode)
		lines := Render([]Row{{Key: "payload", Type: "string", Value: "user │ | • * data"}}, 100)
		glyphs := terminal.Glyphs()
		if !strings.Contains(lines[0], terminal.ColumnSeparator()) || !strings.Contains(lines[1], glyphs.Rule+glyphs.Cross+glyphs.Rule) || !strings.Contains(lines[2], "user │ | • * data") {
			t.Fatal(mode, lines)
		}
	}
}

func TestNativeMapsSlicesAndNumbersAvoidJSONRoundTrips(t *testing.T) {
	type counter int64
	rows := Rows(map[string]any{
		"map":   map[string]uint64{"max": math.MaxUint64},
		"slice": []int64{math.MinInt64, 9007199254740993},
		"json":  json.Number("900719925474099312345"), "alias": counter(42), "float": float32(1.25),
	})
	byKey := map[string]Row{}
	for _, row := range rows {
		byKey[row.Key] = row
	}
	for key, want := range map[string]Row{
		`map["max"]`: {Type: "integer", Value: "18446744073709551615"},
		"slice[0]":   {Type: "integer", Value: "-9223372036854775808"},
		"slice[1]":   {Type: "integer", Value: "9007199254740993"},
		"json":       {Type: "number", Value: "900719925474099312345"},
		"alias":      {Type: "integer", Value: "42"}, "float": {Type: "number", Value: "1.25"},
	} {
		want.Key = key
		if byKey[key] != want {
			t.Fatalf("native %s: %#v", key, byKey[key])
		}
	}
}

func TestMalformedTrailingAndRootValues(t *testing.T) {
	for _, raw := range []string{`{broken`, `{} {}`, `[] trailing`, `{"a":1} null`} {
		if _, err := Parse(raw); err == nil {
			t.Fatal("invalid JSON accepted", raw)
		}
	}
	for _, raw := range []string{"", " \n", `{}`} {
		if rows, err := Parse(raw); err != nil || len(rows) != 0 {
			t.Fatal("empty map state", rows, err)
		}
	}
	for _, raw := range []string{`null`, `true`, `"text"`, `[]`} {
		rows, err := Parse(raw)
		if err != nil || len(rows) != 1 || rows[0].Key != "(root)" {
			t.Fatal("root value lost", raw, rows, err)
		}
	}
}

func TestFullValuesWrapSafelyAtEveryWidth(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "0")
	long := "BEGIN" + strings.Repeat("long-界-value-", 40) + "END"
	for _, width := range []int{1, 12, 24, 39, 40, 60, 120} {
		full := long
		if width == 1 {
			full = strings.ReplaceAll(full, "界", "X")
		}
		lines := Render([]Row{{"long", "string", full}, {"multiline", "string", "first\nsecond"}, {"unsafe", "string", "\x1b]0;title\aSAFE\u202e"}}, width)
		var values []string
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
			if width >= 40 {
				parts := strings.SplitN(ansi.Strip(line), " │ ", 3)
				if len(parts) == 3 {
					values = append(values, parts[2])
				}
			} else {
				values = append(values, ansi.Strip(line))
			}
		}
		text := strings.Join(values, "")
		if !strings.Contains(text, full) || !strings.Contains(text, "firstsecond") || !strings.Contains(text, "SAFE") || strings.Contains(text, "…") || strings.Contains(text, "\x1b") || strings.Contains(text, "\u202e") {
			t.Fatalf("width %d lost or failed to sanitize a full value: %s", width, text)
		}
	}
}
