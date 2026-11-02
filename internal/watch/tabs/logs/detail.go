package logs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

type detail struct {
	env                                                             protocol.Env
	row                                                             apigen.Log
	session                                                         string
	view                                                            viewport.Model
	width, height                                                   int
	connected, loading, missing, uncertain, changedSession, pending bool
	err                                                             error
}

func newDetail(env protocol.Env, row apigen.Log, originSession string) *detail {
	m := &detail{env: env, row: row, session: env.Refresh.Session(), width: 100, height: 24, view: viewport.New(100, 19), loading: true}
	// A queued ShowDetail can arrive after a session transition. The factory's
	// environment is authoritative; never reopen a cached row from the old scope.
	if originSession != m.session || (m.session != "" && row.SessionId != m.session) {
		m.changedSession, m.row = true, apigen.Log{}
		m.content()
		return m
	}
	// A log is immutable and has no ID endpoint. Restrict the durable refresh to
	// its exact timestamp and correlation/service, then compare its full identity.
	query := url.Values{"limit": {"1000"}, "fromNs": {strconv.FormatInt(row.TimestampNs, 10)}, "toNs": {strconv.FormatInt(row.TimestampNs, 10)}}
	if row.TraceId != "" {
		query.Set("traceId", row.TraceId)
	}
	if row.SpanId != "" {
		query.Set("spanId", row.SpanId)
	}
	if row.ServiceName != "" {
		query.Set("service", row.ServiceName)
	}
	env.Refresh.Register(client.Spec{
		Key: client.Key{Owner: env.Owner, Name: "log"}, Resources: []liveevent.Resource{"logs"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Logs(ctx, client.Scope(query, session))
		},
	})
	m.content()
	return m
}
func (m *detail) Init() tea.Cmd { return nil }
func (m *detail) content() {
	if m.changedSession {
		m.view.SetContent("Session changed. Back returns to logs in the current session.")
		return
	}
	attrs := attributeTable(m.row.Attributes, m.view.Width)
	trace, span := clean(m.row.TraceId), clean(m.row.SpanId)
	if trace == "" {
		trace = "(none)"
	}
	if span == "" {
		span = "(none)"
	}
	body := clean(m.row.Body)
	if body == "" {
		body = "(empty body)"
	}
	text := fmt.Sprintf("Service: %s\nSeverity: %s (%g)\nSession: %s\nTimestamp: %s\nReceived at: %s\nTrace ID: %s\nSpan ID: %s\n\nBody\n%s\n\nAttributes\n%s", clean(m.row.ServiceName), severityName(m.row.Severity), m.row.Severity, clean(m.row.SessionId), stamp(m.row.TimestampNs, true), stamp(m.row.ReceivedAt, true), trace, span, body, attrs)
	offset := m.view.YOffset
	m.view.SetContent(ansi.Hardwrap(text, max(1, m.view.Width), true))
	m.view.SetYOffset(offset)
}
func (m *detail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.view.Width, m.view.Height = m.width, max(1, m.height-4)
		m.content()
	case protocol.Connection:
		m.connected = msg.Connected
		if msg.Resync {
			m.pending = true
		}
	case protocol.SessionChanged:
		if msg.ID != m.session {
			m.changedSession, m.session = true, msg.ID
			m.env.Refresh.Remove(client.Key{Owner: m.env.Owner, Name: "log"})
			m.row = apigen.Log{}
			m.content()
		}
	case protocol.Result:
		if msg.Key != (client.Key{Owner: m.env.Owner, Name: "log"}) || m.changedSession {
			break
		}
		m.loading, m.pending, m.err = false, msg.Pending, msg.Err
		if msg.Err != nil {
			break
		}
		data, ok := msg.Data.(client.Envelope[[]apigen.Log])
		if !ok {
			m.err = fmt.Errorf("unexpected log response")
			break
		}
		found := false
		for _, row := range data.Data {
			if identity(row) == identity(m.row) {
				m.row, found = row, true
				break
			}
		}
		m.uncertain = !found && (m.row.TimestampNs <= 0 || len(data.Data) >= 1000 || (data.Meta != nil && data.Meta.Total != nil && *data.Meta.Total > len(data.Data)))
		m.missing = !found && !m.uncertain && !msg.Pending
		m.content()
	case protocol.Live:
		if row, ok := liveRow(msg.Event); ok && !m.changedSession && identity(row) == identity(m.row) {
			m.row, m.missing, m.uncertain = row, false, false
			m.content()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "home", "g":
			m.view.GotoTop()
			return m, nil
		case "end", "G":
			m.view.GotoBottom()
			return m, nil
		}
		if !m.changedSession && (msg.String() == "enter" || msg.String() == "t" || msg.String() == "s") {
			back := protocol.ReturnContext{Tab: protocol.Logs, Selection: identity(m.row)}
			if msg.String() == "s" && m.row.SpanId != "" || msg.String() == "enter" && m.row.TraceId == "" && m.row.SpanId != "" {
				nav := protocol.OpenSpan{TraceID: m.row.TraceId, SpanID: m.row.SpanId, Return: back}
				return m, func() tea.Msg { return nav }
			}
			if msg.String() != "s" && m.row.TraceId != "" {
				nav := protocol.OpenTrace{TraceID: m.row.TraceId, SpanID: m.row.SpanId, Return: back}
				return m, func() tea.Msg { return nav }
			}
		}
		// The viewport is a leaf widget; owner-route any command it returns.
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, m.env.Command(cmd)
	}
	return m, nil
}
func (m *detail) View() string {
	state := "stored log"
	switch {
	case m.changedSession:
		state = "previous session cleared"
	case m.err != nil:
		state = "STALE · Logs API: " + preview(m.err.Error()) + " · retrying automatically"
	case m.loading:
		state = "checking stored log…"
	case m.pending && m.connected:
		state = "resyncing stored log…"
	case m.missing:
		state = "Log expired or removed · showing retained copy"
	case m.uncertain:
		state = "Retained copy · not found within bounded timestamp lookup"
	}
	navigation := "No trace/span correlation on this log."
	if !m.changedSession && m.row.TraceId != "" {
		navigation = "Enter/t trace (selects correlated span)"
	}
	if !m.changedSession && m.row.SpanId != "" {
		if m.row.TraceId == "" {
			navigation = "Enter/s span · no containing trace ID"
		} else {
			navigation += " · s span"
		}
	}
	lines := []string{tui.Title.Render("Full log") + tui.Muted.Render("  ·  "+state), tui.Accent.Render(navigation), tui.Muted.Render("↑↓/PgUp/PgDn scroll · Esc back · expired correlation: Esc restores this log")}
	lines = append(lines, strings.Split(m.view.View(), "\n")...)
	lines = append(lines, tui.Faint.Render(fmt.Sprintf("%d%%", int(m.view.ScrollPercent()*100))))
	return fit(lines, m.width, m.height)
}
