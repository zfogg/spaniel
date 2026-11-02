// Package traces owns the trace/span lists, waterfall and reusable inspector.
package traces

import (
	"context"
	"fmt"
	"net/url"
	"sort"
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

type listRow struct {
	id, trace, span, service, name, attributes, tag string
	start, duration                                 int64
	status, count                                   float64
	n1                                              bool
}

type listModel struct {
	env                                   protocol.Env
	state                                 viewState
	spans                                 bool
	page, total                           int
	rows, frozen                          []listRow
	selected, text, service, input, draft string
	errors, n1, paused                    bool
	width, height                         int
}

// New is the stable traces-tab constructor used by the shell.
func New(env protocol.Env) tea.Model { return newList(env, false) }

// NewSpanList shares list navigation and rendering with the individual spans tab.
func NewSpanList(env protocol.Env) tea.Model { return newList(env, true) }

func newList(env protocol.Env, spans bool) *listModel {
	m := &listModel{env: env, spans: spans, page: 1, total: -1, service: env.Filters.Service,
		errors: env.Filters.ErrorsOnly, n1: env.Filters.N1Only, width: 100, height: 24}
	m.state = newState(env)
	m.register()
	return m
}

func (m *listModel) register() {
	q := url.Values{"page": {strconv.Itoa(m.page)}, "limit": {strconv.Itoa(pageSize)}}
	if m.service != "" {
		q.Set("service", m.service)
	}
	resource := liveevent.Resource("traces")
	if m.spans {
		resource = "spans"
		q.Set("view", "flat")
		q.Set("sort", "time")
	}
	spans := m.spans // immutable worker input; never capture the model in Fetch.
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: "list"}, Resources: []liveevent.Resource{resource},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			if spans {
				return api.Spans(ctx, client.Scope(q, session))
			}
			return api.Traces(ctx, client.Scope(q, session))
		}})
	m.state.loading = true
}

func (m *listModel) Init() tea.Cmd      { return nil }
func (m *listModel) CapturesKeys() bool { return m.input != "" }
func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(0, v.Height)
	case protocol.Connection:
		m.state.connection = v
	case protocol.SessionChanged:
		if m.state.session != v.ID {
			m.state.reset(v.ID)
			m.rows = nil
			m.frozen = nil
			m.selected = ""
			m.page = 1
			m.total = -1
			m.register()
		}
	case protocol.ReturnContext:
		if v.Selection != "" {
			m.selected = v.Selection
		}
		m.ensureSelection()
	case protocol.Result:
		if !m.state.accept(v, m.env.Owner, "list") {
			break
		}
		if v.Err != nil {
			break
		}
		var rows []listRow
		var meta *apigen.Meta
		if m.spans {
			e, ok := v.Data.(client.Envelope[[]apigen.SpanRow])
			if !ok {
				m.state.err = "Unexpected span response"
				break
			}
			meta = e.Meta
			for _, s := range e.Data {
				if m.state.matches(s.SessionId) && (m.service == "" || s.ServiceName == m.service) {
					rows = append(rows, spanListRow(s))
				}
			}
		} else {
			e, ok := v.Data.(client.Envelope[[]apigen.TraceRow])
			if !ok {
				m.state.err = "Unexpected trace response"
				break
			}
			meta = e.Meta
			for _, s := range e.Data {
				if m.state.matches(s.SessionId) && (m.service == "" || s.ServiceName == m.service) {
					rows = append(rows, traceListRow(s))
				}
			}
		}
		m.rows = boundedRows(rows)
		m.total = -1
		if meta != nil && meta.Total != nil {
			m.total = *meta.Total
		}
		m.ensureSelection()
	case protocol.Live:
		if m.page != 1 {
			break
		}
		var row listRow
		var session string
		if m.spans {
			s, ok := eventSpan(v.Event)
			if !ok {
				break
			}
			row, session = spanListRow(spanRow(s)), s.SessionId
		} else {
			s, ok := eventTrace(v.Event)
			if !ok {
				break
			}
			row, session = traceListRow(s), s.SessionId
		}
		if !m.state.matches(session) {
			break
		}
		// Stream rows carry span fields but no derived tag. Keep a hydrated
		// N+1/lint tag until the authoritative list snapshot replaces it.
		if m.spans && row.tag == "" {
			for _, old := range m.rows {
				if old.id == row.id {
					row.tag, row.n1 = old.tag, old.n1
					break
				}
			}
		}
		if m.service != "" && row.service != m.service {
			for i, old := range m.rows {
				if old.id == row.id {
					m.rows = append(m.rows[:i], m.rows[i+1:]...)
					break
				}
			}
		} else {
			m.rows = boundedRows(append([]listRow{row}, m.rows...))
		}
		m.ensureSelection()
	case tea.KeyMsg:
		if m.input != "" {
			switch v.String() {
			case "esc":
				m.input = ""
			case "enter":
				if m.input == "service" {
					m.service = strings.TrimSpace(m.draft)
					m.page = 1
					m.rows = nil
					m.frozen = nil
					m.paused = false
					m.total = -1
					m.register()
				} else {
					m.text = m.draft
				}
				m.input = ""
				m.ensureSelection()
			case "backspace", "ctrl+h":
				r := []rune(m.draft)
				if len(r) > 0 {
					m.draft = string(r[:len(r)-1])
				}
			default:
				if v.Type == tea.KeyRunes && len([]rune(m.draft)) < 256 {
					runes := []rune(m.draft + clean(string(v.Runes)))
					m.draft = string(runes[:min(256, len(runes))])
				}
			}
			return m, nil
		}
		switch v.String() {
		case "/":
			m.input, m.draft = "text", m.text
		case "s":
			m.input, m.draft = "service", m.service
		case "e":
			m.errors = !m.errors
			m.ensureSelection()
		case "n":
			m.n1 = !m.n1
			m.ensureSelection()
		case "p":
			m.paused = !m.paused
			if m.paused {
				m.frozen = append([]listRow(nil), m.rows...)
			} else {
				m.frozen = nil
			}
			m.ensureSelection()
		case "r":
			m.register()
		case "]", "right":
			if m.hasNext() {
				m.page++
				m.paused = false
				m.rows = nil
				m.frozen = nil
				m.selected = ""
				m.register()
			}
		case "[", "left":
			if m.page > 1 {
				m.page--
				m.paused = false
				m.rows = nil
				m.frozen = nil
				m.selected = ""
				m.register()
			}
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgdown":
			m.move(max(1, m.height-6))
		case "pgup":
			m.move(-max(1, m.height-6))
		case "home", "g":
			m.move(-pageSize)
		case "end", "G":
			m.move(pageSize)
		case "enter":
			for _, row := range m.visible() {
				if row.id == m.selected {
					back := protocol.ReturnContext{Tab: protocol.Traces, Selection: row.id}
					if m.spans {
						back.Tab = protocol.Spans
						return m, func() tea.Msg { return protocol.OpenSpan{TraceID: row.trace, SpanID: row.span, Return: back} }
					}
					return m, func() tea.Msg { return protocol.OpenTrace{TraceID: row.trace, Return: back} }
				}
			}
		}
	}
	return m, nil
}

func (m *listModel) visible() []listRow {
	rows := m.rows
	if m.paused {
		rows = m.frozen
	}
	var out []listRow
	for _, r := range rows {
		if m.errors && r.status < 2 {
			continue
		}
		if m.n1 && !r.n1 {
			continue
		}
		if m.text != "" && !strings.Contains(strings.ToLower(r.name+" "+r.service+" "+r.trace+" "+r.span+" "+r.attributes), strings.ToLower(m.text)) {
			continue
		}
		out = append(out, r)
	}
	return out
}
func (m *listModel) ensureSelection() {
	rows := m.visible()
	for _, r := range rows {
		if r.id == m.selected {
			return
		}
	}
	if len(rows) > 0 {
		m.selected = rows[0].id
	} else {
		m.selected = ""
	}
}
func (m *listModel) move(delta int) {
	rows := m.visible()
	if len(rows) == 0 {
		return
	}
	i := 0
	for j, r := range rows {
		if r.id == m.selected {
			i = j
			break
		}
	}
	m.selected = rows[max(0, min(len(rows)-1, i+delta))].id
}
func (m *listModel) hasNext() bool {
	if m.total >= 0 {
		return m.page*pageSize < m.total
	}
	return len(m.rows) == pageSize
}
func (m *listModel) View() string {
	rows := m.visible()
	kind := "traces"
	if m.spans {
		kind = "individual spans"
	}
	count := fmt.Sprintf("%d shown", len(rows))
	if m.total >= 0 {
		count += fmt.Sprintf(" · %d at snapshot", m.total)
	}
	lines := m.state.appendBanner([]string{tui.Title.Render(fmt.Sprintf("%s · page %d · %s", kind, m.page, count))})
	filters := fmt.Sprintf("service: %s · errors: %t · N+1: %t · text: %s", fallback(m.service, "all"), m.errors, m.n1, fallback(m.text, "none"))
	if m.paused {
		filters = "PAUSED · " + filters
	}
	lines = append(lines, clean(filters))
	if m.input != "" {
		lines = append(lines, tui.Accent.Render(m.input+"> "+m.draft+"▏"))
	}
	lines = append(lines, tui.Muted.Render("   STATUS DURATION    SERVICE / OPERATION                         ID"))
	if len(rows) == 0 {
		message := "No matching rows on this page. Adjust filters or choose another page."
		if m.state.loading {
			message = "Loading stored " + kind + "…"
		} else if len(m.rows) == 0 {
			message = "No " + kind + " in this scope (or data expired). Waiting for telemetry…"
		}
		lines = append(lines, message)
	} else {
		cursor := 0
		for i, r := range rows {
			if r.id == m.selected {
				cursor = i
			}
		}
		start, end := rowWindow(len(rows), cursor, max(1, m.height-len(lines)-2))
		for _, r := range rows[start:end] {
			lines = append(lines, renderListRow(r, r.id == m.selected, m.spans, m.width))
		}
	}
	return screen(lines, m.width, m.height)
}

func traceListRow(s apigen.TraceRow) listRow {
	return listRow{id: s.SessionId + "/" + s.TraceId, trace: s.TraceId, service: s.ServiceName, name: s.Name, attributes: s.Attributes, start: s.StartNs, duration: s.DurationNs, status: s.StatusCode, count: s.SpanCount, n1: s.HasN1}
}
func spanListRow(s apigen.SpanRow) listRow {
	tag := ""
	if s.Tag != nil {
		tag = *s.Tag
	}
	return listRow{id: s.SessionId + "/" + s.TraceId + "/" + s.SpanId, trace: s.TraceId, span: s.SpanId, service: s.ServiceName, name: s.Name, attributes: s.Attributes, tag: tag, start: s.StartNs, duration: s.DurationNs, status: s.StatusCode, n1: tag == "n+1"}
}
func boundedRows(rows []listRow) []listRow {
	seen := map[string]bool{}
	out := make([]listRow, 0, min(pageSize, len(rows)))
	for _, r := range rows {
		if !seen[r.id] {
			seen[r.id] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].start != out[j].start {
			return out[i].start > out[j].start
		}
		return out[i].id < out[j].id
	})
	if len(out) > pageSize {
		out = out[:pageSize]
	}
	return out
}

// RenderSpanRow and RenderTraceRow share the list presentation with dashboard result panels.
func RenderSpanRow(row apigen.SpanRow, selected bool, width int) string {
	return renderListRow(spanListRow(row), selected, true, width)
}
func RenderTraceRow(row apigen.TraceRow, selected bool, width int) string {
	return renderListRow(traceListRow(row), selected, false, width)
}
func renderListRow(r listRow, selected, spans bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "› "
	}
	name := clean(r.service + " / " + r.name)
	if spans {
		if r.tag != "" {
			name += " [" + clean(r.tag) + "]"
		}
	} else {
		if r.count >= 0 {
			name += fmt.Sprintf(" (%.0f spans)", r.count)
		} else {
			name += " (span count unavailable)"
		}
		if r.n1 {
			name += " [N+1]"
		}
	}
	line := fmt.Sprintf("%s%-6s %-11s %s", prefix, statusText(r.status), duration(r.duration), name)
	if width >= 78 {
		line = fmt.Sprintf("%s%-6s %-11s %s %s", prefix, statusText(r.status), duration(r.duration), pad(name, max(12, width-53)), short(fallback(r.span, r.trace)))
	}
	line = clip(line, width)
	if r.status >= 2 {
		line = errorStyle.Render(line)
	} else if selected {
		line = tui.SelectedRow.Render(line)
	}
	return line
}
