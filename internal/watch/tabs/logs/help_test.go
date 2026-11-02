package logs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func helpFor(bindings []key.Binding, pressed string) string {
	for _, binding := range bindings {
		for _, candidate := range binding.Keys() {
			if candidate == pressed && binding.Enabled() {
				return binding.Help().Desc
			}
		}
	}
	return ""
}

func TestShortHelpTracksListSelectionPagingPauseAndInput(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*model)
	if helpFor(m.ShortHelp(), "enter") != "" || helpFor(m.ShortHelp(), "]") != "" {
		t.Fatal("loading help offers unavailable actions")
	}
	snapshot(m, env.Owner, "list", logRow("selected", 100))
	m.total = 201
	if helpFor(m.ShortHelp(), "enter") != "full log" || helpFor(m.ShortHelp(), "]") != "next page" || helpFor(m.ShortHelp(), "[") != "" {
		t.Fatal("list help ignores current data/page")
	}
	key(m, "p")
	if helpFor(m.ShortHelp(), "p") != "resume rows" {
		t.Fatal("pause help did not change")
	}
	for _, input := range []string{"/", "s"} {
		key(m, input)
		if !strings.HasPrefix(helpFor(m.ShortHelp(), "enter"), "apply ") || helpFor(m.ShortHelp(), "esc") != "cancel" || helpFor(m.ShortHelp(), "ctrl+u") == "" || helpFor(m.ShortHelp(), "p") != "" {
			t.Fatal("input mode help offers list actions")
		}
		key(m, "esc")
	}
	key(m, "]")
	if helpFor(m.ShortHelp(), "[") != "" {
		t.Fatal("loading page offers navigation")
	}
	snapshot(m, env.Owner, "list", logRow("older", 50))
	if helpFor(m.ShortHelp(), "[") != "previous page" {
		t.Fatal("older page cannot go back")
	}
}

func TestViewsUseShellHeadingAndConnectionStatus(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*model)
	snapshot(m, env.Owner, "list", logRow("stored body", 100))
	d := newDetail(env, logRow("stored body", 100), env.Refresh.Session())
	for _, connected := range []bool{true, false} {
		m.Update(protocol.Connection{Connected: connected})
		view := ansi.Strip(m.View())
		if !strings.HasPrefix(view, "service=") || strings.Contains(view, "live") || strings.Contains(view, "disconnected") || !strings.Contains(view, "stored body") {
			t.Fatal("list duplicated shell heading/connection state or lost data", view)
		}
		d.Update(protocol.Connection{Connected: connected})
		if strings.Contains(d.View(), "disconnected") {
			t.Fatal("inspector duplicated shell connection state")
		}
	}
}

func TestShortHelpTracksCorrelationScrollAndSession(t *testing.T) {
	env := testEnv(t)
	row := logRow(strings.Repeat("multiline body\n", 80), 100)
	d := newDetail(env, row, env.Refresh.Session())
	d.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if helpFor(d.ShortHelp(), "pgdown") == "" || helpFor(d.ShortHelp(), "enter") != "trace with correlated span" || helpFor(d.ShortHelp(), "s") != "correlated span" {
		t.Fatal("full log help misses scrolling/navigation")
	}
	row.TraceId = ""
	d = newDetail(env, row, env.Refresh.Session())
	if helpFor(d.ShortHelp(), "enter") != "correlated span" || helpFor(d.ShortHelp(), "t") != "" {
		t.Fatal("span-only log offers wrong navigation")
	}
	row.SpanId = ""
	d = newDetail(env, row, env.Refresh.Session())
	if helpFor(d.ShortHelp(), "enter") != "" || helpFor(d.ShortHelp(), "s") != "" {
		t.Fatal("uncorrelated log offers navigation")
	}
	d.Update(protocol.SessionChanged{ID: "other"})
	if helpFor(d.ShortHelp(), "enter") != "" {
		t.Fatal("previous-session log offers navigation")
	}
}
