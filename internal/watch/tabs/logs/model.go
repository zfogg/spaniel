// Package logs implements the stored and live log browser and full log inspector.
package logs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

const pageSize = 100

type model struct {
	env                                         protocol.Env
	rows, frozen                                []apigen.Log
	selected                                    string
	page, total, width, height                  int
	service, severity, text, session            string
	editing, input                              string
	loaded, loading, paused, connected, pending bool
	err                                         error
}

// New is the stable shell constructor. All reads use the shared refresh scheduler.
func New(env protocol.Env) tea.Model {
	m := &model{env: env, page: 1, width: 100, height: 24, service: env.Filters.Service, session: env.Refresh.Session()}
	m.register()
	return m
}
func (m *model) register() {
	query := url.Values{"page": {strconv.Itoa(m.page)}, "limit": {strconv.Itoa(pageSize)}}
	if m.service != "" {
		query.Set("service", m.service)
	}
	if m.severity != "" {
		query.Set("severity", m.severity)
	}
	m.loading, m.err = true, nil
	m.env.Refresh.Register(client.Spec{
		Key: client.Key{Owner: m.env.Owner, Name: "list"}, Resources: []liveevent.Resource{"logs"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Logs(ctx, client.Scope(query, session))
		},
	})
}
func (m *model) Init() tea.Cmd      { return nil }
func (m *model) CapturesKeys() bool { return m.editing != "" }
func (m *model) source() []apigen.Log {
	if m.paused {
		return m.frozen
	}
	return m.rows
}
func (m *model) visible() []apigen.Log {
	var out []apigen.Log
	for _, row := range m.source() {
		if matches(row, m.session, m.service, m.severity, m.text, m.env.Filters.ErrorsOnly) {
			out = append(out, row)
		}
	}
	return out
}
func (m *model) selection(rows []apigen.Log) int {
	for i, row := range rows {
		if identity(row) == m.selected {
			return i
		}
	}
	return 0
}
func (m *model) reconcile() {
	rows := m.visible()
	if len(rows) == 0 {
		m.selected = ""
		return
	}
	m.selected = identity(rows[m.selection(rows)])
}
func (m *model) resetPage() {
	m.page = 1
	m.rows, m.frozen = nil, nil
	m.loaded, m.paused, m.selected = false, false, ""
	m.register()
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case protocol.Connection:
		m.connected = msg.Connected
		if msg.Resync {
			m.pending = true
		}
	case protocol.SessionChanged:
		if msg.ID != m.session {
			m.session = msg.ID
			m.resetPage()
		}
	case protocol.Result:
		if msg.Key != (client.Key{Owner: m.env.Owner, Name: "list"}) {
			break
		}
		m.loading, m.pending, m.err = false, msg.Pending, msg.Err
		if msg.Err != nil {
			break
		}
		data, ok := msg.Data.(client.Envelope[[]apigen.Log])
		if !ok {
			m.err = fmt.Errorf("unexpected logs response")
			break
		}
		m.loaded = true
		m.rows = sortedUnique(data.Data, pageSize)
		m.total = len(m.rows)
		if data.Meta != nil && data.Meta.Total != nil {
			m.total = *data.Meta.Total
		}
		m.reconcile()
	case protocol.Live:
		if row, ok := liveRow(msg.Event); ok && m.page == 1 && matches(row, m.session, m.service, m.severity, "", false) {
			m.rows = sortedUnique(append(m.rows, row), pageSize)
			m.reconcile()
		}
	case protocol.ReturnContext:
		if msg.Tab == protocol.Logs {
			m.selected = msg.Selection
			m.reconcile()
		}
	case tea.KeyMsg:
		if m.editing != "" {
			switch msg.String() {
			case "esc":
				m.editing, m.input = "", ""
			case "enter":
				if m.editing == "service" {
					m.service = strings.TrimSpace(m.input)
					m.resetPage()
				} else {
					m.text = strings.TrimSpace(m.input)
				}
				m.editing, m.input = "", ""
				m.reconcile()
			case "backspace", "ctrl+h":
				r := []rune(m.input)
				if len(r) > 0 {
					m.input = string(r[:len(r)-1])
				}
			case "ctrl+u":
				m.input = ""
			default:
				if msg.Type == tea.KeyRunes && len([]rune(m.input)) < 256 {
					m.input += strings.ReplaceAll(clean(string(msg.Runes)), "\n", " ")
				}
			}
			return m, nil
		}
		rows := m.visible()
		i := m.selection(rows)
		switch msg.String() {
		case "up", "k":
			i = max(0, i-1)
		case "down", "j":
			i = min(len(rows)-1, i+1)
		case "pgup":
			i = max(0, i-max(1, m.height-7))
		case "pgdown":
			i = min(len(rows)-1, i+max(1, m.height-7))
		case "home", "g":
			i = 0
		case "end", "G":
			i = len(rows) - 1
		case "/":
			m.editing, m.input = "text on loaded page", m.text
		case "s":
			m.editing, m.input = "service", m.service
		case "v":
			levels := []string{"", "trace", "debug", "info", "warn", "error", "fatal"}
			for n, level := range levels {
				if level == m.severity {
					m.severity = levels[(n+1)%len(levels)]
					break
				}
			}
			m.resetPage()
			return m, nil
		case " ", "p":
			m.paused = !m.paused
			if m.paused {
				m.frozen = append([]apigen.Log(nil), m.rows...)
			} else {
				m.frozen = nil
			}
			m.reconcile()
			return m, nil
		case "[", "]":
			if m.loading {
				return m, nil
			}
			if msg.String() == "[" && m.page > 1 {
				m.page--
			} else if msg.String() == "]" && m.page*pageSize < m.total {
				m.page++
			} else {
				return m, nil
			}
			m.rows, m.frozen, m.selected = nil, nil, ""
			m.loaded, m.paused = false, false
			m.register()
			return m, nil
		case "enter":
			if len(rows) > 0 {
				row := rows[i]
				session := m.session
				back := protocol.ReturnContext{Tab: protocol.Logs, Selection: identity(row)}
				return m, func() tea.Msg {
					return protocol.ShowDetail{Return: back, New: func(env protocol.Env) tea.Model { return newDetail(env, row, session) }}
				}
			}
		}
		if i >= 0 && i < len(rows) {
			m.selected = identity(rows[i])
		}
	}
	return m, nil
}
func (m *model) View() string {
	rows := m.visible()
	// The shell owns the tab heading and connection status. Keep data context
	// separate from the temporary inline legend for the contextual-footer move.
	var lines []string
	service, severity := m.service, m.severity
	if service == "" {
		service = "all"
	}
	if severity == "" {
		severity = "all"
	}
	filters := fmt.Sprintf("service=%s  severity=%s  page=%d  %d loaded / %d stored at snapshot", preview(service), severity, m.page, len(m.rows), m.total)
	if m.env.Filters.ErrorsOnly {
		filters += "  errors only (loaded page)"
	}
	lines = append(lines, tui.Muted.Render(filters))
	if m.editing != "" {
		lines = append(lines, tui.Accent.Render(m.editing+": "+clean(m.input)+"▏"))
		lines = append(lines, tui.Muted.Render("Enter apply · Esc cancel · Ctrl+U clear input"))
	} else {
		lines = append(lines, tui.Muted.Render("↑↓ select · Enter full log · / text (page) · s service · v severity · [ ] pages · p pause"))
	}
	if m.text != "" {
		lines = append(lines, "Text on loaded page: "+clean(m.text))
	}
	if m.paused {
		lines = append(lines, tui.Warn.Render("PAUSED · background log updates retained"))
	}
	if m.pending && m.connected && !m.loading {
		lines = append(lines, "Resyncing stored logs…")
	}
	if m.err != nil {
		lines = append(lines, tui.Danger.Render("Logs API: "+preview(m.err.Error())+" · retrying automatically; retained rows are stale"))
	}
	if m.loading {
		lines = append(lines, "Loading stored logs…")
	}
	if len(rows) == 0 && !m.loading {
		if m.loaded || len(m.rows) > 0 {
			lines = append(lines, "No logs match on this page. Adjust filters or browse stored pages.")
		} else if m.err == nil {
			lines = append(lines, "Waiting for stored logs…")
		}
	}
	if len(rows) > 0 {
		lines = append(lines, tui.Faint.Render("  TIME (local)  LEVEL  SERVICE          BODY / CORRELATION"))
		i := m.selection(rows)
		capacity := max(1, m.height-len(lines))
		start := max(0, i-capacity+1)
		for n := start; n < min(len(rows), start+capacity); n++ {
			prefix := "  "
			if n == i {
				prefix = "› "
			}
			row := rows[n]
			correlation := "t:— s:—"
			if row.TraceId != "" || row.SpanId != "" {
				correlation = "t:" + cell(preview(row.TraceId), 8) + " s:" + cell(preview(row.SpanId), 8)
			}
			level := tui.SeverityStyle(int(row.Severity)).Render(fmt.Sprintf("%-5s", severityName(row.Severity)))
			line := prefix + stamp(row.TimestampNs, false) + "  " + level + "  " + cell(preview(row.ServiceName), 16) + " " + cell(preview(row.Body), max(1, m.width-64)) + "  " + correlation
			if m.width < 70 {
				line = prefix + level + " " + cell(preview(row.ServiceName), 10) + " " + preview(row.Body)
			}
			lines = append(lines, line)
		}
	}
	return fit(lines, m.width, m.height)
}
