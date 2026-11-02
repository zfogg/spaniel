package shell

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"strings"
	"testing"
	"time"
)

type recorder struct {
	owner                       string
	results, live, owned, focus int
	keys                        []string
	capture                     bool
}

func (m *recorder) Init() tea.Cmd      { return nil }
func (m *recorder) View() string       { return m.owner }
func (m *recorder) CapturesKeys() bool { return m.capture }
func (m *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case protocol.Result:
		m.results++
	case protocol.Live:
		m.live++
	case protocol.Focus:
		if msg.Active {
			m.focus++
		}
	case string:
		m.owned++
	case tea.KeyMsg:
		m.keys = append(m.keys, msg.String())
	}
	return m, nil
}

type detailRecorder struct{ models []*recorder }

func (d *detailRecorder) make(env protocol.Env) tea.Model {
	model := &recorder{owner: env.Owner}
	d.models = append(d.models, model)
	return model
}
func (d *detailRecorder) Trace(e protocol.Env, _ protocol.OpenTrace) tea.Model { return d.make(e) }
func (d *detailRecorder) Span(e protocol.Env, _ protocol.OpenSpan) tea.Model   { return d.make(e) }
func fixture(t *testing.T) (*Model, []*recorder, *detailRecorder) {
	t.Helper()
	env := protocol.Env{Context: context.Background(), Refresh: client.NewRefreshes(context.Background(), nil)}
	var tabs []protocol.Tab
	var models []*recorder
	for _, id := range protocol.Order {
		model := &recorder{owner: string(id)}
		models = append(models, model)
		tabs = append(tabs, protocol.Tab{ID: id, Title: string(id), New: func(e protocol.Env) tea.Model {
			e.Refresh.Register(client.Spec{Key: client.Key{Owner: e.Owner, Name: "list"}, Resources: []liveevent.Resource{"spans"}, Fetch: func(context.Context, *client.Client, string) (any, error) { return nil, nil }})
			return model
		}})
	}
	details := &detailRecorder{}
	return New(env, tabs, details, nil, false), models, details
}
func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}
func TestSevenTabsTitleRoutingAndPersistentDetails(t *testing.T) {
	m, models, details := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(m.View(), "spaniel - traces") {
		t.Fatal("initial heading")
	}
	work := m.env.Refresh.Due(time.Now())
	var hidden *client.Work
	for i := range work {
		if work[i].Key.Owner != "traces" {
			hidden = &work[i]
			break
		}
	}
	if hidden == nil {
		t.Fatal("fixture has no hidden request")
	}
	m.Update(protocol.OpenTrace{TraceID: "trace", SpanID: "selected", Return: protocol.ReturnContext{Tab: protocol.Traces, Selection: "return-row"}})
	m.Update(protocol.OpenSpan{TraceID: "trace", SpanID: "selected"})
	if len(m.stack) != 2 {
		t.Fatal("shared navigation did not nest")
	}
	m.Update(hidden.Run())
	for _, model := range models {
		want := 0
		if model.owner == hidden.Key.Owner {
			want = 1
		}
		if model.results != want {
			t.Fatalf("result delivered to wrong tab %s", model.owner)
		}
	}
	m.Update(key("tab"))
	if m.title() != "spaniel - spans" {
		t.Fatal("tab title")
	}
	m.Update(client.Update{Event: &liveevent.Message{Type: "heartbeat"}})
	if details.models[0].live != 1 || details.models[1].live != 1 {
		t.Fatal("hidden detail stopped receiving events")
	}
	m.Update(protocol.Owned{Owner: details.models[1].owner, Message: "timer"})
	if details.models[1].owned != 1 {
		t.Fatal("hidden async result lost")
	}
	m.Update(key("shift+tab"))
	if len(m.stack) != 2 {
		t.Fatal("switching tabs discarded return stack")
	}
	m.Update(key("esc"))
	if len(m.stack) != 1 {
		t.Fatal("escape quit rather than back")
	}
	m.Update(key("esc"))
	if len(m.stack) != 0 {
		t.Fatal("could not return to list")
	}
	for i, id := range protocol.Order {
		if i > 0 {
			m.Update(key("tab"))
		}
		if m.title() != "spaniel - "+string(id) {
			t.Fatalf("tab order: %s", m.title())
		}
	}
	m.Update(key("tab"))
	if m.active != 0 {
		t.Fatal("tab did not wrap")
	}
	models[0].capture = true
	m.Update(key("q"))
	if len(models[0].keys) != 1 {
		t.Fatal("text entry lost q")
	}
}
func TestSnapshotReplayAndReconnectSession(t *testing.T) {
	m, models, _ := fixture(t)
	now := time.Now()
	m.status.Connected = true
	work := m.env.Refresh.Due(now)
	target := work[0]
	model := models[0]
	for _, x := range models {
		if x.owner == target.Key.Owner {
			model = x
		}
	}
	ev := liveevent.Message{Type: "span", Decoded: &liveevent.SpanPayload{TraceID: "t", SpanID: "s"}}
	m.Update(client.Update{Event: &ev})
	m.Update(target.Run())
	if model.results != 1 || model.live != 2 {
		t.Fatal("snapshot overwrote in-flight stream row")
	}
	remaining := m.env.Refresh.Due(now.Add(2 * time.Second))
	if len(remaining) == 0 {
		t.Fatal("dirty repair missing")
	}
	m.Update(client.Update{Event: &liveevent.Message{Type: "active_session", Decoded: &liveevent.ActiveSessionPayload{ID: "other"}}})
	for _, w := range remaining {
		before := model.results
		m.Update(w.Run())
		if model.results != before {
			t.Fatal("stale session result reached model")
		}
	}
}
