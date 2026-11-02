// Package lints displays stored semantic and detector findings using the shared watch protocols.
package lints

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

const rowLimit = 2000

type finding struct {
	id, source, session, trace, span, parent, rule, severity, message, fingerprint, service string
	created, wasted                                                                         int64
	count                                                                                   float64
}

func lintID(w apigen.LintWarning) string {
	return strings.Join([]string{"lint", w.SessionId, w.TraceId, w.SpanId, w.RuleId}, "\x00")
}
func issueID(w apigen.TraceIssue) string {
	if w.Id != "" {
		return "issue\x00" + w.SessionId + "\x00" + w.Id
	}
	return strings.Join([]string{"issue", w.SessionId, w.TraceId, w.Kind, w.Fingerprint}, "\x00")
}
func spanKey(session, trace, span string) string { return session + "\x00" + trace + "\x00" + span }

type sourceState struct {
	loaded, pending bool
	err             error
	sequence        uint64
}
type model struct {
	env                           protocol.Env
	session                       string
	width, height, cursor, scroll int
	lints                         []apigen.LintWarning
	issues                        []apigen.TraceIssue
	services                      map[string]string
	states                        map[string]sourceState
	rows, visible                 []finding
	selected, filter              string
	input                         textinput.Model
	editing                       bool
	connection                    protocol.Connection
	detail                        *finding
	found, wrongSession           bool
	target                        []apigen.Span
}

// New is the stable shell constructor.
func New(env protocol.Env) tea.Model { return newModel(env, nil) }
func newModel(env protocol.Env, detail *finding) *model {
	i := textinput.New()
	i.Prompt = "/ "
	i.CharLimit = 300
	m := &model{env: env, session: env.Refresh.Session(), width: 80, height: 20, services: map[string]string{}, states: map[string]sourceState{}, input: i, detail: detail}
	if env.Filters.Service != "" {
		m.filter = "service:" + env.Filters.Service
	}
	if env.Filters.ErrorsOnly {
		m.filter += " severity:error"
	}
	if env.Filters.N1Only {
		m.filter += " rule:n_plus_one"
	}
	if detail != nil {
		m.selected = detail.id
		m.found = true
	}
	m.register()
	return m
}
func (m *model) register() {
	// Registry mutations run on the UI goroutine; workers capture immutable query state.
	owner := m.env.Owner
	trace := ""
	if m.detail != nil {
		trace = m.detail.trace
	}
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: owner, Name: "lint"}, Resources: []liveevent.Resource{"lint"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Lints(ctx, client.Scope(nil, session))
	}})
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: owner, Name: "issues"}, Resources: []liveevent.Resource{"issues"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		q := url.Values{}
		if trace != "" {
			q.Set("traceId", trace)
		}
		return api.Issues(ctx, client.Scope(q, session))
	}})
	if m.detail == nil {
		m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: owner, Name: "services"}, Resources: []liveevent.Resource{"spans"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Spans(ctx, client.Scope(url.Values{"limit": {"2000"}, "page": {"1"}, "view": {"flat"}}, session))
		}})
	} else if trace != "" {
		m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: owner, Name: "target"}, Resources: []liveevent.Resource{"trace", "spans"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Trace(ctx, trace, client.Scope(nil, session))
		}})
	} else if m.detail.span != "" {
		span := m.detail.span
		m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: owner, Name: "target"}, Resources: []liveevent.Resource{"span"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Span(ctx, span, client.Scope(nil, session))
		}})
	}
}
func (m *model) Init() tea.Cmd      { return nil }
func (m *model) CapturesKeys() bool { return m.editing }
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.input.Width = max(1, m.width-4)
		m.clamp()
	case protocol.Connection:
		m.connection = msg
		if msg.Resync {
			for k, s := range m.states {
				s.pending = true
				m.states[k] = s
			}
		}
	case protocol.SessionChanged:
		if msg.ID == m.session {
			break
		}
		m.session = msg.ID
		m.lints = nil
		m.issues = nil
		m.services = map[string]string{}
		m.target = nil
		m.rows = nil
		m.visible = nil
		m.selected = ""
		m.cursor = 0
		m.scroll = 0
		m.states = map[string]sourceState{}
		if m.detail != nil {
			m.wrongSession = true
			m.found = false
			m.env.Refresh.RemoveOwner(m.env.Owner)
		}
	case protocol.ReturnContext:
		if msg.Tab == protocol.Lints {
			m.selected = msg.Selection
			m.refilter()
		}
	case protocol.Live:
		// The shell already applies the complete invalidation matrix; self telemetry
		// is patch-only and never starts an HTTP refresh here.
		for _, resource := range liveevent.Changes(msg.Event) {
			for _, name := range []string{"lint", "issues", "services", "target"} {
				if ((name == "lint" || name == "issues") && string(resource) == name) || (name == "services" && resource == "spans") || (name == "target" && (resource == "trace" || resource == "spans" || resource == "span")) {
					s := m.states[name]
					s.pending = true
					m.states[name] = s
				}
			}
		}
	case protocol.Result:
		m.result(msg)
	case tea.KeyMsg:
		if m.editing {
			switch msg.Type {
			case tea.KeyEsc:
				m.editing = false
				m.input.Blur()
				return m, nil
			case tea.KeyEnter:
				m.filter = m.input.Value()
				m.editing = false
				m.input.Blur()
				m.refilter()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, m.env.Command(cmd)
		}
		if m.detail != nil {
			switch msg.String() {
			case "up", "k":
				m.scroll = max(0, m.scroll-1)
			case "down", "j":
				m.scroll++
			case "pgdown":
				m.scroll += max(1, m.height-4)
			case "pgup":
				m.scroll = max(0, m.scroll-max(1, m.height-4))
			case "t", "s", "enter":
				return m, m.navigate(msg.String())
			}
			return m, nil
		}
		switch msg.String() {
		case "/":
			m.editing = true
			m.input.SetValue(m.filter)
			m.input.CursorEnd()
			return m, m.env.Command(m.input.Focus())
		case "c":
			m.filter = ""
			m.refilter()
		case "up", "k":
			m.cursor--
		case "down", "j":
			m.cursor++
		case "pgup", "left":
			m.cursor -= m.pageSize()
		case "pgdown", "right":
			m.cursor += m.pageSize()
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(m.visible) - 1
		case "enter":
			if len(m.visible) > 0 {
				row := m.visible[m.cursor]
				back := protocol.ReturnContext{Tab: protocol.Lints, Selection: row.id}
				return m, func() tea.Msg {
					return protocol.ShowDetail{Return: back, New: func(env protocol.Env) tea.Model { return newModel(env, &row) }}
				}
			}
		}
		m.clamp()
		if len(m.visible) > 0 {
			m.selected = m.visible[m.cursor].id
		}
	default:
		if m.editing {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, m.env.Command(cmd)
		}
	}
	return m, nil
}
func (m *model) result(msg protocol.Result) {
	if msg.Key.Owner != m.env.Owner || msg.Session != m.session || m.wrongSession {
		return
	}
	s := m.states[msg.Key.Name]
	if msg.Sequence <= s.sequence {
		return
	}
	s.sequence = msg.Sequence
	s.pending = msg.Pending
	s.err = msg.Err
	if msg.Err == nil {
		s.loaded = true
		switch data := msg.Data.(type) {
		case client.Envelope[[]apigen.LintWarning]:
			if m.detail != nil {
				m.lints = nil
				for _, w := range data.Data {
					if lintID(w) == m.selected && (len(m.lints) == 0 || w.CreatedAt > m.lints[0].CreatedAt) {
						m.lints = []apigen.LintWarning{w}
					}
				}
			} else {
				m.lints = append([]apigen.LintWarning(nil), data.Data...)
				sort.SliceStable(m.lints, func(i, j int) bool { return m.lints[i].CreatedAt > m.lints[j].CreatedAt })
				m.lints = append([]apigen.LintWarning(nil), m.lints[:min(len(m.lints), rowLimit)]...)
			}
		case client.Envelope[[]apigen.TraceIssue]:
			if m.detail != nil {
				m.issues = nil
				for _, w := range data.Data {
					if issueID(w) == m.selected || (m.detail.source == "semantic" && w.SessionId == m.detail.session && w.TraceId == m.detail.trace && w.ExampleSpanId == m.detail.span && w.Kind == m.detail.rule) {
						if len(m.issues) == 0 || w.CreatedAt > m.issues[0].CreatedAt {
							m.issues = []apigen.TraceIssue{w}
						}
					}
				}
			} else {
				m.issues = append([]apigen.TraceIssue(nil), data.Data...)
				sort.SliceStable(m.issues, func(i, j int) bool { return m.issues[i].CreatedAt > m.issues[j].CreatedAt })
				m.issues = append([]apigen.TraceIssue(nil), m.issues[:min(len(m.issues), rowLimit)]...)
			}
		case client.Envelope[[]apigen.SpanRow]:
			m.services = map[string]string{}
			for _, w := range data.Data[:min(len(data.Data), rowLimit)] {
				m.services[spanKey(w.SessionId, w.TraceId, w.SpanId)] = w.ServiceName
			}
		case client.Envelope[[]apigen.Span]:
			m.target = nil
			m.services = map[string]string{}
			for _, w := range data.Data {
				if m.detail == nil || w.SessionId != m.detail.session || w.TraceId != m.detail.trace {
					continue
				}
				// Retain only existence witnesses, not a second trace cache.
				if len(m.target) == 0 || (w.SpanId == m.detail.span && m.target[0].SpanId != w.SpanId) {
					m.target = append(m.target, apigen.Span{SessionId: w.SessionId, TraceId: w.TraceId, SpanId: w.SpanId})
				}
				if w.SpanId == m.detail.span {
					m.services[spanKey(w.SessionId, w.TraceId, w.SpanId)] = w.ServiceName
				}
			}
		case client.Envelope[apigen.Span]:
			m.target = nil
			m.services = map[string]string{}
			w := data.Data
			if m.detail != nil && w.SessionId == m.detail.session && w.SpanId == m.detail.span {
				m.target = []apigen.Span{{SessionId: w.SessionId, TraceId: w.TraceId, SpanId: w.SpanId}}
				m.services[spanKey(w.SessionId, w.TraceId, w.SpanId)] = w.ServiceName
			}
		}
	}
	m.states[msg.Key.Name] = s
	m.rebuild()
}
func (m *model) rebuild() {
	rows := make(map[string]finding, len(m.lints)+len(m.issues))
	detector := map[string]bool{}
	for _, w := range m.issues {
		detector[lintID(apigen.LintWarning{SessionId: w.SessionId, TraceId: w.TraceId, SpanId: w.ExampleSpanId, RuleId: w.Kind})] = true
		if old, ok := rows[issueID(w)]; ok && old.created >= w.CreatedAt {
			continue
		}
		rows[issueID(w)] = finding{id: issueID(w), source: "detector", session: w.SessionId, trace: w.TraceId, span: w.ExampleSpanId, parent: w.ParentSpanId, rule: w.Kind, severity: "warning", message: fmt.Sprintf("%s: %g occurrences", w.Kind, w.Count), fingerprint: w.Fingerprint, count: w.Count, wasted: w.WastedNs, created: w.CreatedAt}
	}
	for _, w := range m.lints {
		if detector[lintID(w)] {
			continue
		}
		if old, ok := rows[lintID(w)]; ok && old.created >= w.CreatedAt {
			continue
		}
		rows[lintID(w)] = finding{id: lintID(w), source: "semantic", session: w.SessionId, trace: w.TraceId, span: w.SpanId, rule: w.RuleId, severity: w.Severity, message: w.Message, count: 1, created: w.CreatedAt}
	}
	m.rows = nil
	for _, row := range rows {
		row.service = m.services[spanKey(row.session, row.trace, row.span)]
		m.rows = append(m.rows, row)
	}
	sort.Slice(m.rows, func(i, j int) bool {
		if m.rows[i].created != m.rows[j].created {
			return m.rows[i].created > m.rows[j].created
		}
		return m.rows[i].id < m.rows[j].id
	})
	if m.detail == nil {
		m.rows = m.rows[:min(len(m.rows), rowLimit)]
	} else {
		// /lint includes detector summaries without a source discriminator. If
		// Enter raced the richer /issues snapshot, upgrade that open summary.
		for _, row := range m.rows {
			if m.detail.source == "semantic" && row.source == "detector" && row.session == m.detail.session && row.trace == m.detail.trace && row.span == m.detail.span && row.rule == m.detail.rule {
				*m.detail = row
				m.selected = row.id
			}
		}
		name := "lint"
		if m.detail.source == "detector" {
			name = "issues"
		}
		if m.states[name].loaded && m.states[name].err == nil {
			m.found = false
			for _, row := range m.rows {
				if row.id == m.selected {
					*m.detail = row
					m.found = true
					break
				}
			}
		}
		// Some legacy findings carry only a span ID. Resolve its containing
		// trace through the public API without altering the finding identity.
		if m.detail.trace == "" && m.detail.span != "" {
			for _, span := range m.target {
				if span.SpanId == m.detail.span && span.SessionId == m.detail.session {
					m.detail.trace = span.TraceId
					m.detail.service = m.services[spanKey(span.SessionId, span.TraceId, span.SpanId)]
				}
			}
		}
	}
	m.refilter()
}
func matches(row finding, filter string) bool {
	for _, word := range strings.Fields(strings.ToLower(filter)) {
		field, value, typed := strings.Cut(word, ":")
		text := strings.Join([]string{row.rule, row.message, row.fingerprint, row.service, row.trace, row.span, row.source, row.severity}, " ")
		if typed {
			switch field {
			case "severity":
				text = row.severity
			case "rule", "kind":
				text = row.rule
			case "service":
				text = row.service
			case "source":
				text = row.source
			default:
				value = word
			}
		} else {
			value = word
		}
		if !strings.Contains(strings.ToLower(text), value) {
			return false
		}
	}
	return true
}
func (m *model) refilter() {
	if m.detail != nil {
		return
	}
	m.visible = nil
	for _, row := range m.rows {
		if matches(row, m.filter) {
			m.visible = append(m.visible, row)
		}
	}
	for i, row := range m.visible {
		if row.id == m.selected {
			m.cursor = i
			break
		}
	}
	m.clamp()
	if len(m.visible) > 0 {
		m.selected = m.visible[m.cursor].id
	}
}
func (m *model) pageSize() int { return max(1, (m.height-7)/2) }
func (m *model) clamp()        { m.cursor = max(0, min(m.cursor, len(m.visible)-1)) }
func (m *model) navigate(key string) tea.Cmd {
	if m.detail == nil || !m.found || m.wrongSession || !m.connection.Connected || m.detail.trace == "" {
		return nil
	}
	s := m.states["target"]
	if !s.loaded || s.err != nil || s.pending {
		return nil
	}
	name := "lint"
	if m.detail.source == "detector" {
		name = "issues"
	}
	if state := m.states[name]; !state.loaded || state.err != nil || state.pending {
		return nil
	}
	traceExists, spanExists := false, false
	for _, w := range m.target {
		if w.TraceId == m.detail.trace && w.SessionId == m.detail.session {
			traceExists = true
			if w.SpanId == m.detail.span {
				spanExists = true
			}
		}
	}
	back := protocol.ReturnContext{Tab: protocol.Lints, Selection: m.detail.id}
	row := *m.detail
	if key == "s" || (key == "enter" && row.span != "") {
		if !spanExists || row.span == "" {
			return nil
		}
		return func() tea.Msg { return protocol.OpenSpan{TraceID: row.trace, SpanID: row.span, Return: back} }
	}
	if !traceExists {
		return nil
	}
	return func() tea.Msg { return protocol.OpenTrace{TraceID: row.trace, SpanID: row.span, Return: back} }
}

// Terminal control bytes in telemetry must never become terminal commands.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func created(ns int64) string {
	if ns == 0 {
		return "unknown"
	}
	return time.Unix(0, ns).UTC().Format(time.RFC3339Nano)
}
