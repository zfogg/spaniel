package alerts

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func bindingFor(bindings []key.Binding, input string) *key.Binding {
	for _, binding := range bindings {
		for _, candidate := range binding.Keys() {
			if candidate == input {
				return &binding
			}
		}
	}
	return nil
}
func TestShortHelpFollowsFocusAndInputMode(t *testing.T) {
	_, env := newFixture(t)
	m := New(env).(*model)
	if bindingFor(m.ShortHelp(), "enter") != nil || bindingFor(m.ShortHelp(), "[") != nil || bindingFor(m.ShortHelp(), "]") != nil {
		t.Fatal("empty first page advertises unavailable navigation")
	}
	drain(t, env, m)
	if b := bindingFor(m.ShortHelp(), "enter"); b == nil || b.Help().Desc != "open rule" {
		t.Fatal("loaded list omits rule navigation")
	}
	m.total = 50
	if bindingFor(m.ShortHelp(), "]") == nil {
		t.Fatal("available next page omitted")
	}
	press(m, "/")
	if bindingFor(m.ShortHelp(), "esc") == nil || bindingFor(m.ShortHelp(), "ctrl+u") == nil || bindingFor(m.ShortHelp(), "s") != nil || bindingFor(m.ShortHelp(), "v") != nil {
		t.Fatal("search help includes inactive list actions")
	}
	press(m, "esc")
	env.Owner = "detail:1"
	d := newDetail(env, "latency").(*model)
	drain(t, env, d)
	if bindingFor(d.ShortHelp(), "/") != nil || bindingFor(d.ShortHelp(), "enter") != nil || bindingFor(d.ShortHelp(), "i") == nil {
		t.Fatal("definition help not scoped to view")
	}
	press(d, "i")
	if b := bindingFor(d.ShortHelp(), "enter"); b == nil || b.Help().Desc != "inspect instance" {
		t.Fatal("instance help missing")
	}
	show := press(d, "enter")().(protocol.ShowDetail)
	env.Owner = "detail:2"
	inspection := show.New(env).(*model)
	drain(t, env, inspection)
	if bindingFor(inspection.ShortHelp(), "enter") != nil || bindingFor(inspection.ShortHelp(), "/") != nil || bindingFor(inspection.ShortHelp(), "i") == nil {
		t.Fatal("nested instance help not scoped to inspection")
	}
	press(d, "h")
	press(d, "s")
	drain(t, env, d)
	if b := bindingFor(d.ShortHelp(), "s"); b == nil || b.Help().Desc != "state: pending" {
		t.Fatal("history filter state missing from help")
	}
	show = press(d, "enter")().(protocol.ShowDetail)
	env.Owner = "detail:3"
	inspection = show.New(env).(*model)
	drain(t, env, inspection)
	if bindingFor(inspection.ShortHelp(), "enter") != nil || bindingFor(inspection.ShortHelp(), "h") == nil {
		t.Fatal("nested event help not scoped to inspection")
	}
	for _, b := range inspection.ShortHelp() {
		for _, input := range b.Keys() {
			if input == "q" || input == "tab" || input == "esc" {
				t.Fatal("feature advertises shell-owned bindings")
			}
		}
	}
}
