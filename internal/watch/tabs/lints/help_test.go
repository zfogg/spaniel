package lints

import (
	"strings"
	"testing"

	keys "github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

var _ interface{ ShortHelp() []keys.Binding } = (*model)(nil)

func hasHelp(m *model, press string) bool {
	for _, binding := range m.ShortHelp() {
		if keys.Matches(key(press), binding) {
			return true
		}
	}
	return false
}
func TestContextualShortHelp(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	if !hasHelp(m, "/") || hasHelp(m, "enter") || hasHelp(m, "c") {
		t.Fatal("empty list help contains unavailable actions")
	}
	seed(m)
	if !hasHelp(m, "enter") || !hasHelp(m, "j") {
		t.Fatal("populated list help missing navigation")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	if !hasHelp(m, "right") || !hasHelp(m, "end") {
		t.Fatal("paged list help missing paging")
	}
	m.Update(key("/"))
	if !m.CapturesKeys() || !hasHelp(m, "enter") || !hasHelp(m, "esc") || hasHelp(m, "/") || hasHelp(m, "j") {
		t.Fatal("filter input help leaked list actions")
	}
	m.Update(key("rule:http"))
	m.Update(key("enter"))
	if !hasHelp(m, "c") {
		t.Fatal("active filter has no clear action")
	}
	d := openDetail(t, m)
	if hasHelp(d, "t") || hasHelp(d, "s") || hasHelp(d, "/") {
		t.Fatal("loading detail offers unvalidated actions")
	}
	seed(d)
	target(d)
	if !hasHelp(d, "t") || !hasHelp(d, "s") || !hasHelp(d, "enter") {
		t.Fatal("correlated detail help missing shared navigation")
	}
	d.Update(protocol.Connection{Connected: false})
	if hasHelp(d, "t") || hasHelp(d, "s") {
		t.Fatal("stale detail help offers unavailable navigation")
	}
	d.Update(protocol.Connection{Connected: true})
	deliver(d, "target", client.Envelope[[]apigen.Span]{})
	if hasHelp(d, "t") || hasHelp(d, "s") {
		t.Fatal("expired target help offers navigation")
	}
	d.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if !hasHelp(d, "j") {
		t.Fatal("overflowing detail has no scrolling help")
	}
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if hasHelp(d, "j") {
		t.Fatal("fitting detail offers meaningless scrolling")
	}
	m.filter = "rule:service.name"
	m.refilter()
	absent := openDetail(t, m)
	seed(absent)
	if hasHelp(absent, "t") || hasHelp(absent, "s") {
		t.Fatal("uncorrelated finding help offers navigation")
	}
	warnings, _, _ := fixtures()
	warnings[0].SpanId = ""
	m.filter = ""
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: warnings[:1]})
	traceOnly := openDetail(t, m)
	deliver(traceOnly, "lint", client.Envelope[[]apigen.LintWarning]{Data: warnings[:1]})
	target(traceOnly)
	if !hasHelp(traceOnly, "enter") || !hasHelp(traceOnly, "t") || hasHelp(traceOnly, "s") {
		t.Fatal("trace-only finding help does not match Enter behavior")
	}
}

func TestReadableTimestampPreservesInternalPrecision(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	d := openDetail(t, m)
	seed(d)
	if d.detail.created != 1793631000000000001 {
		t.Fatal("timestamp precision changed internally")
	}
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := d.View()
	if !strings.Contains(view, "Created: 2026-11-02 14:50:00.000 UTC") || strings.Contains(view, "1793631000000000001") || strings.Contains(view, ".000000001") {
		t.Fatal(view)
	}
	if created(0) != "unknown" {
		t.Fatal("missing timestamp rendered as epoch")
	}
}
