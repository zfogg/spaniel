package logs

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

func TestAttributeTablePreservesTypesExactNumbersAndNestedValues(t *testing.T) {
	raw := `{"large":18446744073709551615,"precise":1.234567890123456789,"flag":true,"text":"true","nothing":null,"nested":{"child":"value"},"array":[7,"entry",{}],"empty":[]}`
	table := attributeTable(raw, 120)
	for _, expected := range []string{"KEY", "TYPE", "VALUE", "18446744073709551615", "1.234567890123456789", "boolean", "string", "null", `nested["child"]`, "object", "array[0]", "array[1]", "array[2]", "0 fields", "0 items"} {
		if !strings.Contains(table, expected) {
			t.Fatalf("attributes table lost %q: %s", expected, table)
		}
	}
	if strings.Contains(table, `{"large"`) {
		t.Fatal("rendered serialized attributes")
	}
}

func TestAttributeTableWrapsFullValuesAndHandlesMalformedData(t *testing.T) {
	long := strings.Repeat("abcdefghij", 30) + "END"
	for _, width := range []int{1, 12, 31, 32, 60, 120} {
		table := attributeTable(`{"long":"`+long+`","lines":"first\nsecond","nested":{"x":42}}`, width)
		for _, line := range strings.Split(table, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("table exceeds %d columns: %q", width, line)
			}
		}
		if width >= 32 && (!strings.Contains(table, "END") || !strings.Contains(table, "first") || !strings.Contains(table, "second")) {
			t.Fatal("long/multiline value truncated", table)
		}
	}
	for _, raw := range []string{`{"broken":`, `{} {}`} {
		if !strings.Contains(attributeTable(raw, 100), "Invalid attribute JSON") {
			t.Fatal("malformed attributes silently discarded")
		}
	}
	if attributeTable("", 100) != "(none)" || attributeTable("{}", 100) != "(none)" {
		t.Fatal("empty attributes lack useful state")
	}
}

func TestTimestampPresentationUsesMillisecondsAndKeepsInternalPrecision(t *testing.T) {
	ns := time.Date(2026, 11, 2, 17, 30, 0, 123456789, time.UTC).UnixNano()
	full := stamp(ns, true)
	if !strings.Contains(full, ".123 ") || strings.Contains(full, "123456789") || !strings.Contains(full[len(full)-6:], ":") {
		t.Fatal("timestamp is not a readable millisecond date/time with timezone", full)
	}
	if !strings.HasSuffix(stamp(ns, false), ".123") {
		t.Fatal("list timestamp precision")
	}
	row := logRow("body", ns)
	other := row
	other.TimestampNs++
	if identity(row) == identity(other) || sortedUnique([]apigen.Log{row, other}, 100)[0].TimestampNs != ns+1 {
		t.Fatal("display rounding changed internal precision")
	}
	d := newDetail(testEnv(t), row, "session")
	if strings.Contains(d.View(), "Timestamp ns:") || strings.Contains(d.View(), "Received at ns:") || strings.Contains(d.View(), ".123456789") {
		t.Fatal("inspector exposes raw nanosecond timestamps", d.View())
	}
}
