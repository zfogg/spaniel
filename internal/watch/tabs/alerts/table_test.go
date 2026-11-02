package alerts

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	attributeview "github.com/zfogg/spaniel/internal/watch/attributes"
)

func TestRuleInstanceAndHistoryTablesAlignAndResize(t *testing.T) {
	_, env := newFixture(t)
	m := New(env).(*model)
	drain(t, env, m)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	var header, row string
	for _, line := range lines {
		if strings.Contains(line, "STATE") && strings.Contains(line, "SEVERITY") {
			header = line
		}
		if strings.HasPrefix(line, "> firing") {
			row = line
		}
	}
	for heading, value := range map[string]string{"STATE": "firing", "SEVERITY": "critical", "NAME": "Checkout latency", "INSTANCES": "2"} {
		if strings.Index(header, heading) < 0 || strings.Index(header, heading) != strings.Index(row, value) {
			t.Fatalf("column %s not aligned:\n%s\n%s", heading, header, row)
		}
	}
	env.Owner = "detail:1"
	d := newDetail(env, "latency").(*model)
	drain(t, env, d)
	for _, section := range []string{"instances", "history"} {
		d.section = section
		d.reconcile()
		d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		view := ansi.Strip(d.View())
		if !strings.Contains(view, "STATE") || !strings.Contains(view, "GROUP") || !strings.Contains(view, " │ ") {
			t.Fatalf("%s is not tabular:\n%s", section, view)
		}
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 1, Height: 1}, {Width: 18, Height: 5}, {Width: 40, Height: 15}, {Width: 70, Height: 20}, {Width: 120, Height: 30}} {
		for _, view := range []*model{m, d} {
			view.Update(size)
			for _, line := range strings.Split(view.View(), "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatalf("table width overflow %d", size.Width)
				}
			}
		}
	}
	if got := stamp(1793640000123456789, time.Unix(0, 1793640000123456789).Add(2*time.Minute)); got != "2026-11-02 17:20:00 UTC * 2m ago" {
		t.Fatal(got)
	}
	if got := duration(1234567); got != "1.23ms" {
		t.Fatal(got)
	}
}

func TestAttributeTablesPreserveTypesPrecisionAndFullValues(t *testing.T) {
	raw := `{"service.name":"checkout","number":9007199254740993,"flag":true,"nested":{"items":["first",9223372036854775807,null]},"empty":[]}`
	rows, err := attributeview.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]attributeview.Row{}
	for _, row := range rows {
		found[row.Key] = row
	}
	for key, want := range map[string]attributeview.Row{
		"number":             {Key: "number", Type: "number", Value: "9007199254740993"},
		`nested["items"][1]`: {Key: `nested["items"][1]`, Type: "number", Value: "9223372036854775807"},
		"flag":               {Key: "flag", Type: "boolean", Value: "true"},
		`nested["items"][2]`: {Key: `nested["items"][2]`, Type: "null", Value: "null"},
		"empty":              {Key: "empty", Type: "array", Value: "0 items"},
	} {
		if found[key] != want {
			t.Fatalf("attribute %s: %#v", key, found[key])
		}
	}
	_, env := newFixture(t)
	m := newModel(env, "latency")
	for _, width := range []int{18, 40, 80, 120} {
		m.width = width
		table := m.attributeTable(raw)
		for _, line := range table {
			if width >= 40 && ansi.StringWidth(line) >= width {
				t.Fatalf("attribute width overflow: %q", line)
			}
		}
		joined := strings.Join(table, "\n")
		if !strings.Contains(strings.ReplaceAll(joined, "\n", ""), "true") {
			t.Fatal("typed value missing")
		}
	}
	m.width = 80
	full := "begin " + strings.Repeat("word ", 100) + "full-value-end"
	lines := m.attributeTable(`{"long":"` + full + `"}`)
	m.height = 12
	m.offset = 1000000
	tail := strings.Join(m.scroll(lines), "\n")
	if !strings.Contains(tail, "full-value-end") {
		t.Fatal("long attribute value not retained through scrolling")
	}
}
