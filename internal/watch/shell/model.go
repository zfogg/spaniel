// Package shell owns terminal lifecycle, tabs, shared detail navigation and
// routing. It never renders feature content or opens per-tab sockets.
package shell

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/terminal"
	"strings"
	"time"
)

type pane struct {
	owner  string
	model  tea.Model
	back   protocol.ReturnContext
	cancel context.CancelFunc
}
type Model struct {
	env                    protocol.Env
	tabs                   []protocol.Tab
	models                 []tea.Model
	details                protocol.Details
	stack                  []pane
	saved                  map[protocol.TabID][]pane
	active, width, height  int
	serial                 uint64
	updates                <-chan client.Update
	status                 client.Status
	fixedSession           bool
	helpOffset, bodyHeight int
	helpOpen               bool
}
type streamClosed struct{}

func New(env protocol.Env, tabs []protocol.Tab, details protocol.Details, updates <-chan client.Update, fixedSession bool) *Model {
	if env.Time == nil {
		env.Time = protocol.NewClock(time.Now())
	}
	m := &Model{env: env, tabs: tabs, details: details, updates: updates, fixedSession: fixedSession, saved: map[protocol.TabID][]pane{}}
	for _, tab := range tabs {
		e := env
		e.Owner = string(tab.ID)
		m.models = append(m.models, tab.New(e))
	}
	m.bodyHeight = m.contentHeight()
	return m
}
func wait(ch <-chan client.Update) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		u, ok := <-ch
		if !ok {
			return streamClosed{}
		}
		return u
	}
}
func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return protocol.Tick{Time: t} })
}
func (m *Model) title() string { return "spaniel - " + m.tabs[m.active].Title }
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{wait(m.updates), tick(), tea.SetWindowTitle(m.title())}
	for _, model := range m.models {
		cmds = append(cmds, model.Init())
	}
	cmds = append(cmds, m.route(string(m.tabs[0].ID), protocol.Focus{Active: true}))
	return tea.Batch(cmds...)
}
func (m *Model) route(owner string, msg tea.Msg) tea.Cmd {
	for i, tab := range m.tabs {
		if string(tab.ID) == owner {
			next, cmd := m.models[i].Update(msg)
			m.models[i] = next
			return cmd
		}
	}
	for i := range m.stack {
		if m.stack[i].owner == owner {
			next, cmd := m.stack[i].model.Update(msg)
			m.stack[i].model = next
			return cmd
		}
	}

	for id, panes := range m.saved {
		for i := range panes {
			if panes[i].owner == owner {
				next, cmd := panes[i].model.Update(msg)
				panes[i].model = next
				m.saved[id] = panes
				return cmd
			}
		}
	}
	return nil
}
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, tab := range m.tabs {
		cmds = append(cmds, m.route(string(tab.ID), msg))
	}
	for _, p := range m.stack {
		cmds = append(cmds, m.route(p.owner, msg))
	}
	for _, panes := range m.saved {
		for _, p := range panes {
			cmds = append(cmds, m.route(p.owner, msg))
		}
	}
	return tea.Batch(cmds...)
}
func (m *Model) owner() string {
	if len(m.stack) > 0 {
		return m.stack[len(m.stack)-1].owner
	}
	return string(m.tabs[m.active].ID)
}
func (m *Model) focused() tea.Model {
	if len(m.stack) > 0 {
		return m.stack[len(m.stack)-1].model
	}
	return m.models[m.active]
}
func (m *Model) flush(now time.Time) tea.Cmd {
	if !m.status.Connected {
		return nil
	}
	var cmds []tea.Cmd
	for _, work := range m.env.Refresh.Due(now) {
		cmds = append(cmds, func() tea.Msg { return work.Run() })
	}
	return tea.Batch(cmds...)
}
func (m *Model) session(id string) tea.Cmd {
	if m.fixedSession || id == m.env.Refresh.Session() {
		return nil
	}
	m.env.Refresh.SetSession(id)
	return m.broadcast(protocol.SessionChanged{ID: id})
}
func (m *Model) open(factory protocol.Factory, back protocol.ReturnContext) tea.Cmd {
	m.serial++
	env := m.env
	detailCtx, cancel := context.WithCancel(m.env.Context)
	env.Context = detailCtx
	env.Owner = fmt.Sprintf("detail:%d", m.serial)
	oldOwner := m.owner()
	m.stack = append(m.stack, pane{owner: env.Owner, model: factory(env), back: back, cancel: cancel})
	// Seed lifecycle state synchronously before Init or focused input can use it.
	commands := []tea.Cmd{
		m.route(oldOwner, protocol.Focus{Active: false}),
		m.route(env.Owner, protocol.Connection(m.status)),
		m.route(env.Owner, protocol.SessionChanged{ID: m.env.Refresh.Session()}),
		m.route(env.Owner, tea.WindowSizeMsg{Width: m.width, Height: m.contentHeight()}),
	}
	commands = append(commands, m.stack[len(m.stack)-1].model.Init(), m.route(env.Owner, protocol.Focus{Active: true}), m.flush(time.Now()))
	return tea.Batch(commands...)
}
func (m *Model) back() tea.Cmd {
	if len(m.stack) == 0 {
		return nil
	}
	last := m.stack[len(m.stack)-1]
	if last.cancel != nil {
		last.cancel()
	}
	m.env.Refresh.RemoveOwner(last.owner)
	m.stack = m.stack[:len(m.stack)-1]
	return tea.Batch(m.route(m.owner(), protocol.Focus{Active: true}), m.route(m.owner(), last.back))
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.helpContext()
	_, cmd := m.update(msg)
	if before != m.helpContext() {
		m.helpOffset = 0
	}
	// Help changes after input, snapshots and navigation as well as resize.
	// Notify retained panes too, so their viewports never cover the footer.
	height := m.contentHeight()
	if height != m.bodyHeight {
		m.bodyHeight = height
		cmd = tea.Batch(cmd, m.broadcast(tea.WindowSizeMsg{Width: m.width, Height: height}))
	}
	return m, cmd
}
func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case protocol.Owned:
		return m, m.route(msg.Owner, msg.Message)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bodyHeight = m.contentHeight()
		return m, m.broadcast(tea.WindowSizeMsg{Width: msg.Width, Height: m.bodyHeight})
	case client.Update:
		cmds := []tea.Cmd{wait(m.updates)}
		if msg.Status != nil {
			m.status = *msg.Status
			if m.status.Resync {
				m.env.Refresh.Resync()
			}
			cmds = append(cmds, m.broadcast(protocol.Connection(*msg.Status)), m.flush(time.Now()))
		}
		if msg.Event != nil {
			e := *msg.Event
			if p, ok := e.Decoded.(*liveevent.ActiveSessionPayload); ok {
				cmds = append(cmds, m.session(p.ID))
			}
			m.env.Refresh.Observe(e)
			cmds = append(cmds, m.broadcast(protocol.Live{Event: e}))
		}
		return m, tea.Batch(cmds...)
	case protocol.Result:
		if !m.env.Refresh.Complete(msg, time.Now()) {
			return m, m.flush(time.Now())
		}
		if msg.Key.Owner == "$shell" {
			if value, ok := msg.Data.(client.Envelope[apigen.ActiveSession]); ok && msg.Err == nil {
				return m, tea.Batch(m.session(value.Data.Id), m.flush(time.Now()))
			}
			return m, nil
		}
		msg.Pending = m.env.Refresh.Pending(msg.Key)
		cmds := []tea.Cmd{m.route(msg.Key.Owner, msg)}
		for _, event := range m.env.Refresh.DrainReplay(msg.Key) {
			cmds = append(cmds, m.route(msg.Key.Owner, protocol.Live{Event: event}))
		}
		return m, tea.Batch(cmds...)
	case protocol.Tick:
		return m, tea.Batch(tick(), m.broadcast(msg), m.flush(msg.Time))
	case protocol.OpenTrace:
		return m, m.open(func(env protocol.Env) tea.Model { return m.details.Trace(env, msg) }, msg.Return)
	case protocol.OpenSpan:
		return m, m.open(func(env protocol.Env) tea.Model { return m.details.Span(env, msg) }, msg.Return)
	case protocol.ShowDetail:
		return m, m.open(msg.New, msg.Return)
	case protocol.Back:
		return m, m.back()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.helpOpen {
			return m, m.popupKey(msg)
		}
		if msg.Type == tea.KeyF2 || (msg.String() == "?" && !m.capturesKeys()) {
			m.helpOpen = true
			m.helpOffset = 0
			return m, nil
		}
		if capture, ok := m.focused().(protocol.KeyCapture); ok && capture.CapturesKeys() {
			return m, m.route(m.owner(), msg)
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "esc", "backspace":
			if len(m.stack) > 0 {
				return m, m.back()
			}
		case "tab", "shift+tab":
			old := m.owner()
			direction := 1
			if msg.String() == "shift+tab" {
				direction = len(m.tabs) - 1
			}
			m.active = (m.active + direction) % len(m.tabs)
			oldID := m.tabs[(m.active-direction+len(m.tabs))%len(m.tabs)].ID
			m.saved[oldID] = m.stack
			m.stack = m.saved[m.tabs[m.active].ID]
			delete(m.saved, m.tabs[m.active].ID)
			return m, tea.Batch(m.route(old, protocol.Focus{Active: false}), m.route(m.owner(), protocol.Focus{Active: true}), tea.SetWindowTitle(m.title()))
		}
	case streamClosed:
		m.status.Connected = false
		return m, nil
	}
	// Non-key async feature messages must carry an owner via protocol.Result.
	// Untargeted messages are broadcast so hidden timers cannot hit a wrong tab.
	if _, ok := msg.(tea.KeyMsg); ok {
		return m, m.route(m.owner(), msg)
	}
	return m, m.broadcast(msg)
}
func (m *Model) View() string {
	if m.env.Time != nil {
		m.env.Time.Set(time.Now())
	}
	labels := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		labels[i] = tui.Muted.Render(t.Title)
		if i == m.active {
			labels[i] = tui.Title.Underline(true).Render("[" + t.Title + "]")
		}
	}
	width := m.width
	if width <= 0 {
		width = 100
	}
	separator := tui.Faint.Render("  " + terminal.Glyphs().Pipe + "  ")
	header := ansi.Truncate(tui.Ink.Bold(true).Render(m.title())+separator+m.connectionLabel()+separator+strings.Join(labels, "  "), width, "…")
	lines := []string{header}
	height := m.contentHeight()
	body := strings.Split(m.focused().View(), "\n")
	for i := 0; i < height; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	if len(lines) > 1 {
		copy(lines[1:], m.overlayHelp(lines[1:]))
	}
	lines = append(lines, m.footer()...)
	if m.height > 0 {
		lines = lines[:min(len(lines), m.height)]
	}
	return strings.Join(lines, "\n")
}
