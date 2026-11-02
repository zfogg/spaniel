package traces

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	attributeview "github.com/zfogg/spaniel/internal/watch/attributes"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

const attributeFixture = `{"big":900719925474099312345,"decimal":1.234567890123456789,"enabled":true,"empty":"","nothing":null,"text":"001","nested":{"region":"east","attempt":2},"tags":["retry",false,{"code":504}]}`

func TestAttributeTablesPreserveScalarTypesAndNestedValues(t *testing.T) {
	rows, err := attributeview.Parse(attributeFixture)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]attributeview.Row{}
	for _, row := range rows {
		byKey[strings.TrimSpace(row.Key)] = row
	}
	for k, want := range map[string]attributeview.Row{
		"big": {Type: "number", Value: "900719925474099312345"}, "decimal": {Type: "number", Value: "1.234567890123456789"},
		"enabled": {Type: "boolean", Value: "true"}, "empty": {Type: "string", Value: "(empty string)"}, "nothing": {Type: "null", Value: "null"}, "text": {Type: "string", Value: "001"},
		"nested": {Type: "object", Value: "2 fields"}, `nested["region"]`: {Type: "string", Value: "east"}, "tags": {Type: "array", Value: "3 items"}, `tags[2]["code"]`: {Type: "number", Value: "504"},
	} {
		got, ok := byKey[k]
		if !ok || got.Type != want.Type || got.Value != want.Value {
			t.Fatalf("%s: %#v, want %#v", k, got, want)
		}
	}
	view := ansi.Strip(strings.Join(attributeLines("Attributes", attributeFixture, 120), "\n"))
	for _, want := range []string{"key/value table", "KEY", "TYPE", "VALUE", "900719925474099312345", "1.234567890123456789", "boolean", "[0]", `nested["region"]`} {
		if !strings.Contains(view, want) {
			t.Fatalf("table omitted %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `"big":`) || strings.Contains(view, `{"`) {
		t.Fatal("attributes rendered as serialized JSON")
	}
	for _, raw := range []string{`{broken`, `{"a":1} {"b":2}`} {
		if !strings.Contains(strings.Join(attributeLines("Attributes", raw, 60), "\n"), "Invalid attributes JSON") {
			t.Fatal("invalid JSON not explicit")
		}
	}
	if _, err := attributeview.Parse(`{}`); err != nil {
		t.Fatal(err)
	}
}

func TestBothListEntryPathsAndExternalNavigationUseSameAttributeTables(t *testing.T) {
	s := sampleSpan("span", "", 1793631000000000000)
	s.Attributes = attributeFixture
	s.Resource = `{"service.name":"api","deployment.revision":18446744073709551615}`
	trace := NewDetails().Trace(testEnv("trace-view"), protocol.OpenTrace{TraceID: s.TraceId}).(*traceModel)
	trace.Update(result("trace-view", "trace", 1, client.Envelope[[]apigen.Span]{Data: []apigen.Span{s}}))
	_, fromTrace := trace.Update(press("enter"))
	list := newList(testEnv("spans"), true)
	list.Update(result("spans", "list", 1, client.Envelope[[]apigen.SpanRow]{Data: []apigen.SpanRow{spanRow(s)}}))
	_, fromSpans := list.Update(press("enter"))
	paths := []protocol.OpenSpan{fromTrace().(protocol.OpenSpan), fromSpans().(protocol.OpenSpan), {TraceID: s.TraceId, SpanID: s.SpanId}}
	var first string
	for i, target := range paths {
		owner := fmt.Sprintf("detail:%d", i)
		model, ok := NewDetails().Span(testEnv(owner), target).(*spanModel)
		if !ok {
			t.Fatal("entry path did not use shared span inspector")
		}
		model.width = 120
		model.Update(result(owner, "span", 1, client.Envelope[apigen.Span]{Data: s}))
		content := ansi.Strip(strings.Join(model.content(), "\n"))
		for _, want := range []string{"Attributes · key/value table", "Resource · key/value table", "KEY", "TYPE", "VALUE", "900719925474099312345", "18446744073709551615"} {
			if !strings.Contains(content, want) {
				t.Fatalf("entry path %d omitted %q", i, want)
			}
		}
		if i == 0 {
			first = content
		} else if content != first {
			t.Fatal("entry paths diverged from the shared table renderer")
		}
	}
}

func TestAttributeValuesRemainFullyScrollableAfterNarrowResize(t *testing.T) {
	m := NewDetails().Span(testEnv("detail:1"), protocol.OpenSpan{TraceID: "trace", SpanID: "span"}).(*spanModel)
	s := sampleSpan("span", "", 1793631000000000000)
	s.Attributes = `{"long":"BEGIN` + strings.Repeat("value-", 80) + `TAIL","multiline":"first\nsecond","unsafe":"\u001b]0;bad\u0007safe"}`
	s.Resource = `{"service.name":"checkout","revision":900719925474099312345}`
	m.Update(result(m.env.Owner, "span", 1, client.Envelope[apigen.Span]{Data: s}))
	for _, width := range []int{18, 40, 120, 12} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 9})
		m.Update(press("home"))
		foundTail, foundRevision := false, false
		for offset := 0; offset < len(m.content()); offset++ {
			view := ansi.Strip(m.View())
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatal("table overflowed terminal")
				}
			}
			// A value can continue across a column boundary between frames.
			flat := strings.NewReplacer("\n", "", " ", "", "│", "").Replace(view)
			if strings.Contains(flat, "TAIL") {
				foundTail = true
			}
			if strings.Contains(flat, "revision") {
				foundRevision = true
			}
			m.Update(press("down"))
		}
		if !foundTail || !foundRevision {
			t.Fatalf("width %d lost full-value scrolling: tail=%t revision=%t", width, foundTail, foundRevision)
		}
	}
	if strings.Contains(strings.Join(m.content(), "\n"), "\x1b]0;") {
		t.Fatal("attribute injected terminal control")
	}
}

func TestReadableTimestampPresentationKeepsInternalPrecision(t *testing.T) {
	const ns int64 = 1793631000123456789
	if got := stamp(ns); got != "2026-11-02 14:50:00.123 UTC" {
		t.Fatalf("timestamp: %s", got)
	}
	s := sampleSpan("span", "", ns)
	content := strings.Join(spanContent(s, 120), "\n")
	if strings.Contains(content, fmt.Sprint(ns)) || strings.Contains(content, " ns)") || strings.Contains(content, ".123456789") {
		t.Fatal("raw nanosecond timestamps remain in normal display")
	}
	if s.StartNs != ns || s.EndNs != ns+100 {
		t.Fatal("presentation changed stored timing precision")
	}
}

func helpHas(bindings []key.Binding, k, description string) bool {
	for _, b := range bindings {
		for _, bindingKey := range b.Keys() {
			if bindingKey == k && strings.Contains(b.Help().Desc, description) {
				return true
			}
		}
	}
	return false
}
func TestContextualShortHelpForListsDetailsAndInput(t *testing.T) {
	list := newList(testEnv("spans"), true)
	if helpHas(list.ShortHelp(), "enter", "") {
		t.Fatal("empty list advertised Enter")
	}
	s := sampleSpan("span", "", 1000)
	list.Update(result("spans", "list", 1, client.Envelope[[]apigen.SpanRow]{Data: []apigen.SpanRow{spanRow(s)}}))
	if !helpHas(list.ShortHelp(), "enter", "open") {
		t.Fatal("populated list omitted open")
	}
	list.Update(press("p"))
	if !helpHas(list.ShortHelp(), "p", "resume") {
		t.Fatal("pause help did not change")
	}
	list.Update(press("s"))
	help := list.ShortHelp()
	if !helpHas(help, "enter", "apply service filter") || !helpHas(help, "esc", "cancel") || helpHas(help, "p", "") {
		t.Fatal("input help retained list actions")
	}
	trace := NewDetails().Trace(testEnv("detail:trace"), protocol.OpenTrace{TraceID: "trace"}).(*traceModel)
	if helpHas(trace.ShortHelp(), "enter", "") {
		t.Fatal("empty trace advertised inspector")
	}
	trace.Update(spanEvent(s))
	if !helpHas(trace.ShortHelp(), "enter", "inspect span") {
		t.Fatal("trace help omitted inspector")
	}
	inspector := NewDetails().Span(testEnv("detail:span"), protocol.OpenSpan{TraceID: "trace", SpanID: "span"}).(*spanModel)
	if !helpHas(inspector.ShortHelp(), "t", "containing trace") || helpHas(inspector.ShortHelp(), "pgdown", "") {
		t.Fatal("missing-span help is not contextual")
	}
	inspector.Update(spanEvent(s))
	if !helpHas(inspector.ShortHelp(), "pgdown", "page") {
		t.Fatal("inspector help omitted scrolling")
	}
	inspector.state.err = "offline"
	if !helpHas(inspector.ShortHelp(), "r", "retry") {
		t.Fatal("error help omitted retry")
	}
}

func TestRequestedSpanDoesNotShrinkLargeSnapshotCapacity(t *testing.T) {
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace", SpanID: "early"}).(*traceModel)
	snapshot := make([]apigen.Span, liveSpanWindow+2)
	snapshot[0] = sampleSpan("early", "", 1000)
	for i := 1; i < len(snapshot); i++ {
		snapshot[i] = sampleSpan(fmt.Sprintf("span-%05d", i), "early", int64(1000+i))
	}
	m.Update(result(m.env.Owner, "trace", 1, client.Envelope[[]apigen.Span]{Data: snapshot}))
	capacity := len(snapshot) + liveSpanWindow
	if m.liveLimit != capacity {
		t.Fatalf("selection shrank capacity to %d, want %d", m.liveLimit, capacity)
	}
	m.Update(spanEvent(sampleSpan("new", "early", 50000)))
	updated := snapshot[0]
	updated.Name = "updated early span"
	m.Update(spanEvent(updated))
	if len(m.spans) != len(snapshot)+1 || m.selected != "seed/early" {
		t.Fatal("live patch dropped authoritative spans/selection")
	}
	if m.spans[0].Name != "updated early span" {
		t.Fatal("existing span patch lost")
	}
	m.truncated = true
	m.pendingSpan = "early"
	m.selectExisting()
	if !m.truncated || m.liveLimit != capacity {
		t.Fatal("selection resolution cleared truncation/capacity")
	}
}
