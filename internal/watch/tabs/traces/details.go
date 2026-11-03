package traces

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/metadata"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

// NewDetails supplies the same inspector for spans, waterfalls and external
// OpenTrace/OpenSpan messages from logs, lints and alerts. No nested program runs.
func NewDetails() protocol.Details { return details{} }

type details struct{}

func (details) Trace(env protocol.Env, target protocol.OpenTrace) tea.Model {
	m := &traceModel{env: env, state: newState(env), traceID: target.TraceID, selected: target.SpanID, pendingSpan: target.SpanID, liveLimit: liveSpanWindow, width: 100, height: 24}
	m.register()
	return m
}
func (details) Span(env protocol.Env, target protocol.OpenSpan) tea.Model {
	m := &spanModel{env: env, state: newState(env), logState: newState(env), traceID: target.TraceID, spanID: target.SpanID, width: 100, height: 24}
	m.register()
	return m
}

type traceModel struct {
	env                            protocol.Env
	state                          viewState
	traceID, selected, pendingSpan string
	spans, frozen                  []apigen.Span
	paused                         bool
	truncated                      bool
	liveLimit                      int
	width, height                  int
}

const liveSpanWindow = 20000

func (m *traceModel) patch(s apigen.Span) {
	for _, old := range m.spans {
		if spanKey(old) == spanKey(s) {
			s = patchSpan(old, s)
			break
		}
	}
	rows := client.MergeByID(m.spans, []apigen.Span{s}, m.liveLimit+1, spanKey)
	if len(rows) > m.liveLimit {
		// A bounded live window must not evict the selected row merely because
		// more spans arrive. The next authoritative snapshot restores full data.
		evict := 0
		if spanKey(rows[evict]) == m.selected {
			evict++
		}
		rows = append(rows[:evict], rows[evict+1:]...)
		m.truncated = true
	}
	m.spans = rows
}

func (m *traceModel) register() {
	id := m.traceID
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: "trace"}, Resources: []liveevent.Resource{"trace"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Trace(ctx, id, client.Scope(nil, session))
	}})
	m.state.loading = true
}
func (m *traceModel) Init() tea.Cmd { return nil }
func (m *traceModel) rows() []flatSpan {
	if m.paused {
		return flatten(m.frozen)
	}
	return flatten(m.spans)
}
func (m *traceModel) selectExisting() {
	rows := m.rows()
	for _, r := range rows {
		if m.pendingSpan != "" && r.span.SpanId == m.pendingSpan {
			m.selected = spanKey(r.span)
			m.pendingSpan = ""
			return
		}
	}
	for _, r := range rows {
		if spanKey(r.span) == m.selected {
			return
		}
	}
	for _, r := range rows {
		if r.span.SpanId == m.selected {
			m.selected = spanKey(r.span)
			return
		}
	}
	// Keep a requested span ID while an empty trace is still arriving.
	if len(rows) > 0 {
		m.selected = spanKey(rows[0].span)
	}
}
func (m *traceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(0, v.Height)
	case protocol.Connection:
		m.state.connection = v
	case protocol.SessionChanged:
		if m.state.session != v.ID {
			m.state.reset(v.ID)
			m.spans = nil
			m.frozen = nil
			m.selected = ""
			m.pendingSpan = ""
			m.liveLimit, m.truncated = liveSpanWindow, false
		}
	case protocol.ReturnContext:
		if v.Selection != "" {
			m.selected = v.Selection
			m.pendingSpan = ""
		}
		m.selectExisting()
	case protocol.Result:
		if !m.state.accept(v, m.env.Owner, "trace") {
			break
		}
		if v.Err != nil {
			var httpErr *client.HTTPError
			if errors.As(v.Err, &httpErr) && httpErr.Status == 404 {
				m.spans = nil
				m.frozen = nil
			}
			break
		}
		e, ok := v.Data.(client.Envelope[[]apigen.Span])
		if !ok {
			m.state.err = "Unexpected trace response"
			break
		}
		m.spans = nil
		for _, s := range e.Data {
			if s.TraceId == m.traceID && m.state.matches(s.SessionId) {
				m.spans = append(m.spans, s)
			}
		}
		m.spans = dedupeSpans(m.spans)
		// Preserve every HTTP span, even in traces larger than the live window.
		m.liveLimit, m.truncated = len(m.spans)+liveSpanWindow, false
		m.selectExisting()
	case protocol.Live:
		s, ok := eventSpan(v.Event)
		if ok && s.TraceId == m.traceID && m.state.matches(s.SessionId) {
			m.patch(s)
			m.selectExisting()
		}
	case tea.KeyMsg:
		rows := m.rows()
		cursor := 0
		for i, r := range rows {
			if spanKey(r.span) == m.selected {
				cursor = i
			}
		}
		delta := 0
		switch v.String() {
		case "up", "k":
			delta = -1
		case "down", "j":
			delta = 1
		case "pgup":
			delta = -max(1, m.height-12)
		case "pgdown":
			delta = max(1, m.height-12)
		case "home", "g":
			delta = -len(rows)
		case "end", "G":
			delta = len(rows)
		case "p":
			m.paused = !m.paused
			if m.paused {
				m.frozen = append([]apigen.Span(nil), m.spans...)
			} else {
				m.frozen = nil
			}
			m.selectExisting()
		case "r":
			m.register()
		case "enter":
			if len(rows) > 0 {
				s := rows[cursor].span
				return m, func() tea.Msg {
					return protocol.OpenSpan{TraceID: s.TraceId, SpanID: s.SpanId, Return: protocol.ReturnContext{Selection: spanKey(s)}}
				}
			}
		}
		if delta != 0 && len(rows) > 0 {
			m.pendingSpan = ""
			m.selected = spanKey(rows[max(0, min(len(rows)-1, cursor+delta))].span)
		}
	}
	return m, nil
}
func (m *traceModel) View() string {
	rows := m.rows()
	start, end := traceWindow(rows)
	title := "Trace waterfall"
	if m.paused {
		title = "PAUSED · " + title
	}
	lines := m.state.appendBanner([]string{tui.RenderContent(tui.ContentTitle, title)})
	lines = append(lines, metadata.Table([]metadata.Field{
		{Name: "Trace ID", Value: m.traceID, Style: tui.ContentStrong}, {Name: "Session", Value: m.state.session},
		{Name: "Spans", Value: fmt.Sprint(len(rows))}, {Name: "Duration", Value: duration(end - start)},
		{Name: "Start", Value: stamp(start, m.env.Now())}, {Name: "End", Value: stamp(end, m.env.Now())},
	}, m.width)...)
	if len(rows) == 0 {
		text := "No spans in this trace/session. Data may have expired; waiting for telemetry…"
		if m.state.loading {
			text = "Loading trace…"
		}
		lines = append(lines, text)
	} else {
		// Leave room for the waterfall heading, selected row and context below.
		flameHeight := min(6, max(0, m.height-len(lines)-5))
		lines = append(lines, renderFlame(rows, m.selected, m.width, flameHeight)...)
		lines = append(lines, waterfallHeader(m.width, start, end))
		cursor := 0
		for i, r := range rows {
			if spanKey(r.span) == m.selected {
				cursor = i
			}
		}
		a, b := rowWindow(len(rows), cursor, max(1, m.height-len(lines)-1))
		for _, r := range rows[a:b] {
			lines = append(lines, waterfallRow(r, spanKey(r.span) == m.selected, start, end, m.width))
		}
		footer := fmt.Sprintf("%d–%d / %d · missing parents marked ? · incomplete timing marked !", a+1, b, len(rows))
		if m.truncated {
			footer = "Live window full · complete trace available on reload · " + footer
		}
		lines = append(lines, tui.RenderContent(tui.ContentLabel, footer))
	}
	return screen(lines, m.width, m.height)
}

type spanModel struct {
	env                   protocol.Env
	state, logState       viewState
	traceID, spanID       string
	span                  *apigen.Span
	logs                  []apigen.Log
	width, height, offset int
}

func (m *spanModel) register() {
	id := m.spanID
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: "span"}, Resources: []liveevent.Resource{"span"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Span(ctx, id, client.Scope(nil, session))
	}})
	m.registerLogs()
	m.state.loading = true
}

func (m *spanModel) registerLogs() {
	q := url.Values{"spanId": {m.spanID}, "limit": {"100"}, "page": {"1"}}
	if m.traceID != "" {
		q.Set("traceId", m.traceID)
	}
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: "logs"}, Resources: []liveevent.Resource{"logs"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Logs(ctx, client.Scope(q, session))
	}})
	m.logState.loading = true
}

func (m *spanModel) setSpan(s apigen.Span, refineLogs bool) {
	m.span = &s
	if m.traceID == "" && s.TraceId != "" {
		m.traceID = s.TraceId
		m.logs = nil
		// A self_* patch must never restart HTTP work. Snapshot hydration may
		// refine the original span-only log query to its containing trace.
		if refineLogs {
			m.registerLogs()
		}
	}
}

func (m *spanModel) Init() tea.Cmd { return nil }
func (m *spanModel) matches(s apigen.Span) bool {
	return s.SpanId == m.spanID && (m.traceID == "" || s.TraceId == m.traceID) && m.state.matches(s.SessionId)
}
func (m *spanModel) matchesLog(l apigen.Log) bool {
	return l.SpanId == m.spanID && (m.traceID == "" || l.TraceId == m.traceID) && m.state.matches(l.SessionId)
}
func (m *spanModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(0, v.Height)
	case protocol.Connection:
		m.state.connection = v
		m.logState.connection = v
	case protocol.SessionChanged:
		if m.state.session != v.ID {
			m.state.reset(v.ID)
			m.logState.reset(v.ID)
			m.span = nil
			m.logs = nil
			m.offset = 0
		}
	case protocol.Result:
		switch v.Key.Name {
		case "span":
			if !m.state.accept(v, m.env.Owner, "span") {
				break
			}
			if v.Err != nil {
				var httpErr *client.HTTPError
				if errors.As(v.Err, &httpErr) && httpErr.Status == 404 {
					m.span = nil
				}
				break
			}
			e, ok := v.Data.(client.Envelope[apigen.Span])
			if !ok {
				m.state.err = "Unexpected span response"
				break
			}
			if !m.matches(e.Data) {
				m.span = nil
				m.state.err = "Span unavailable in this trace/session (missing or expired)"
				break
			}
			s := e.Data
			m.setSpan(s, true)
		case "logs":
			if !m.logState.accept(v, m.env.Owner, "logs") {
				break
			}
			if v.Err != nil {
				break
			}
			e, ok := v.Data.(client.Envelope[[]apigen.Log])
			if !ok {
				m.logState.err = "Unexpected logs response"
				break
			}
			m.logs = nil
			for _, l := range e.Data {
				if m.matchesLog(l) {
					m.logs = append(m.logs, l)
				}
			}
			m.logs = mergeLogs(m.logs)
		}
	case protocol.Live:
		if s, ok := eventSpan(v.Event); ok && m.matches(s) {
			if m.span != nil {
				s = patchSpan(*m.span, s)
			}
			m.setSpan(s, false)
		}
		if l, ok := eventLog(v.Event); ok && m.matchesLog(l) {
			m.logs = mergeLogs(append([]apigen.Log{l}, m.logs...))
		}
	case tea.KeyMsg:
		switch v.String() {
		case "up", "k":
			m.offset = max(0, m.offset-1)
		case "down", "j":
			m.offset++
		case "pgdown", " ":
			m.offset += max(1, m.contentHeight()-1)
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.contentHeight()-1))
		case "home", "g":
			m.offset = 0
		case "end", "G":
			m.offset = max(0, len(m.content())-m.contentHeight())
		case "r":
			m.register()
		case "t":
			if m.traceID != "" {
				trace, span := m.traceID, m.spanID
				return m, func() tea.Msg { return protocol.OpenTrace{TraceID: trace, SpanID: span} }
			}
		}
	}
	m.offset = min(m.offset, max(0, len(m.content())-m.contentHeight()))
	return m, nil
}
func (m *spanModel) content() []string {
	if m.span == nil {
		if m.state.loading {
			return []string{"Loading span…"}
		}
		return []string{"Span missing, unavailable in this session, or expired. The originating view is retained."}
	}
	lines := spanContent(*m.span, m.width, m.env.Now())
	lines = append(lines, "", tui.RenderContent(tui.ContentTitle, "Associated logs · latest 100"))
	lines = m.logState.appendBanner(lines)
	if len(m.logs) == 0 {
		if m.logState.loading {
			lines = append(lines, "Loading associated logs…")
		} else {
			lines = append(lines, "No associated logs available.")
		}
	}
	for _, l := range m.logs {
		text := tui.RenderContent(tui.ContentLabel, stamp(l.TimestampNs, m.env.Now())) + " · " + tui.RenderServiceName(l.ServiceName, clean(l.ServiceName)) + tui.RenderContent(tui.ContentSeverityStyle(int(l.Severity)), fmt.Sprintf(" · severity %.0f", l.Severity)) + " · " + tui.RenderContent(tui.ContentValue, clean(l.Body))
		lines = append(lines, text)
		lines = append(lines, attributeLines("Log attributes", l.Attributes, m.width)...)
	}
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, max(1, m.width), true), "\n")...)
	}
	return wrapped
}
func (m *spanModel) header() []string {
	return m.state.appendBanner([]string{tui.RenderContent(tui.ContentTitle, "Span "+clean(m.spanID))})
}
func (m *spanModel) contentHeight() int { return max(1, m.height-len(m.header())) }
func (m *spanModel) View() string {
	lines := m.header()
	content := m.content()
	start := min(m.offset, max(0, len(content)-m.contentHeight()))
	end := min(len(content), start+m.contentHeight())
	lines = append(lines, content[start:end]...)
	return screen(lines, m.width, m.height)
}
