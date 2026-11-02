// Package alerts implements the read-only alert rule and lifecycle browser.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

const pageSize = 25

var states = []string{"", "firing", "pending", "resolved", "error", "disabled", "acknowledged"}
var severities = []string{"", "critical", "warning", "info"}

type loadState struct {
	loaded, pending bool
	err             error
	sequence        uint64
}

type model struct {
	env                        protocol.Env
	id, section, session       string
	width, height, page        int
	rules                      []apigen.AlertRule
	rule                       apigen.AlertRule
	events                     []apigen.AlertEvent
	silences                   []apigen.AlertSilence
	total                      int
	loads                      map[string]loadState
	selected, group, notice    string
	instance, event, link      string
	state, severity, search    string
	hstate, hseverity, hsearch string
	editing                    bool
	draft                      []rune
	offset                     int
	connection                 protocol.Connection
}

// New is the stable shell constructor. All subscriptions use the shared stream
// and scheduler; no timers, mutation requests or private transport are needed.
func New(env protocol.Env) tea.Model {
	m := newModel(env, "")
	m.registerList()
	return m
}
func newModel(env protocol.Env, id string) *model {
	return &model{env: env, id: id, section: "definition", session: env.Refresh.Session(),
		width: 80, height: 22, page: 1, loads: make(map[string]loadState)}
}
func newDetail(env protocol.Env, id string) tea.Model {
	m := newModel(env, id)
	m.registerRule()
	m.registerHistory()
	return m
}
func (m *model) Init() tea.Cmd      { return nil }
func (m *model) CapturesKeys() bool { return m.editing }
func (m *model) register(name string, fetch client.Fetch) {
	s := m.loads[name]
	s.pending, s.err = true, nil
	m.loads[name] = s
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: name},
		Resources: []liveevent.Resource{"alerts"}, Fetch: fetch})
}
func (m *model) registerList() {
	q := url.Values{"page": {strconv.Itoa(m.page)}, "limit": {strconv.Itoa(pageSize)}, "search": {m.search}}
	m.register("list", func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Alerts(ctx, client.Scope(q, session))
	})
}
func (m *model) registerRule() {
	id := m.id
	m.register("rule", func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Alert(ctx, id, client.Scope(nil, session))
	})
	// Rule events are the canonical source of silence metadata. History has its
	// own subscription so a partial failure cannot erase this metadata.
	m.register("silences", func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.AlertEvents(ctx, id, client.Scope(url.Values{"limit": {"1"}}, session))
	})
}
func (m *model) registerHistory() {
	q := url.Values{"rule_id": {m.id}, "page": {strconv.Itoa(m.page)}, "limit": {strconv.Itoa(pageSize)},
		"search": {m.hsearch}, "severity": {m.hseverity}, "group_key": {m.group}}
	if m.hstate == "acknowledged" {
		q.Set("kind", m.hstate)
	} else {
		q.Set("state", m.hstate)
	}
	m.register("history", func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.AlertHistory(ctx, client.Scope(q, session))
	})
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case protocol.Connection:
		m.connection = msg
		if msg.Resync {
			for key, state := range m.loads {
				state.pending = true
				m.loads[key] = state
			}
		}
	case protocol.SessionChanged:
		if msg.ID != m.session {
			m.session = msg.ID
			m.rules, m.events, m.silences = nil, nil, nil
			m.rule = apigen.AlertRule{}
			m.selected, m.instance, m.event, m.link, m.group = "", "", "", "", ""
			m.page, m.offset, m.total = 1, 0, 0
			m.loads = make(map[string]loadState)
			if m.id == "" {
				m.registerList()
			} else {
				m.registerRule()
				m.registerHistory()
			}
		}
	case protocol.ReturnContext:
		if msg.Tab == protocol.Alerts && m.id == "" {
			m.selected = msg.Selection
			m.reconcile()
		}
	case protocol.Live:
		for _, resource := range liveevent.Changes(msg.Event) {
			if resource == "alerts" {
				for key, s := range m.loads {
					s.pending = true
					m.loads[key] = s
				}
				break
			}
		}
	case protocol.Result:
		m.result(msg)
	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}
func (m *model) result(r protocol.Result) {
	if r.Key.Owner != m.env.Owner || r.Session != m.session {
		return
	}
	s, ok := m.loads[r.Key.Name]
	if !ok || (r.Sequence != 0 && r.Sequence <= s.sequence) {
		return
	}
	s.sequence, s.pending, s.err = r.Sequence, r.Pending, r.Err
	if r.Err == nil {
		switch r.Key.Name {
		case "list":
			if e, ok := r.Data.(client.Envelope[apigen.AlertList]); ok {
				m.rules = nil
				for _, row := range e.Data.Items {
					rule := apigen.AlertRule{Id: row.Id, Name: row.Name, Enabled: row.Enabled,
						Severity: apigen.AlertRuleSeverity(row.Severity), LastError: row.LastError,
						QuerySql: row.QuerySql, LastEvaluatedAt: row.LastEvaluatedAt, Instances: row.Instances}
					m.rules = append(m.rules, rule)
				}
				m.total = total(e.Meta, len(m.rules))
			} else {
				s.err = errors.New("unexpected rule-list response")
			}
		case "rule":
			if e, ok := r.Data.(client.Envelope[apigen.AlertRule]); ok {
				m.rule = e.Data
			} else {
				s.err = errors.New("unexpected rule response")
			}
		case "history":
			if e, ok := r.Data.(client.Envelope[[]apigen.AlertEvent]); ok {
				m.events, m.total = e.Data, total(e.Meta, len(e.Data))
			} else {
				s.err = errors.New("unexpected history response")
			}
		case "silences":
			if e, ok := r.Data.(client.Envelope[[]apigen.AlertEvent]); ok {
				m.silences = e.Silences
			} else {
				s.err = errors.New("unexpected silence response")
			}
		}
		if s.err == nil {
			s.loaded = true
		}
	}
	m.loads[r.Key.Name] = s
	if m.page > 1 && s.err == nil && (r.Key.Name == "list" || r.Key.Name == "history") && (m.page-1)*pageSize >= m.total {
		m.page, m.offset = max(1, (m.total+pageSize-1)/pageSize), 0
		if m.id == "" {
			m.registerList()
		} else {
			m.registerHistory()
		}
	}
	m.reconcile()
}
func total(meta *apigen.Meta, fallback int) int {
	if meta != nil && meta.Total != nil {
		return *meta.Total
	}
	return fallback
}
func (m *model) key(k tea.KeyMsg) tea.Cmd {
	if m.editing {
		switch k.String() {
		case "esc":
			m.editing = false
		case "enter":
			m.editing, m.offset = false, 0
			if m.id == "" {
				m.search, m.page = string(m.draft), 1
				m.rules, m.selected = nil, ""
				m.registerList()
			} else if m.section == "history" {
				m.hsearch, m.page = string(m.draft), 1
				m.events, m.event = nil, ""
				m.registerHistory()
			} else {
				m.search = string(m.draft)
			}
		case "backspace", "ctrl+h":
			if len(m.draft) > 0 {
				m.draft = m.draft[:len(m.draft)-1]
			}
		case "ctrl+u":
			m.draft = nil
		default:
			if k.Type == tea.KeyRunes && len(m.draft)+len(k.Runes) <= 200 {
				m.draft = append(m.draft, k.Runes...)
			}
		}
		m.reconcile()
		return nil
	}
	m.notice = ""
	switch k.String() {
	case "/":
		if m.id == "" || m.section == "history" || m.section == "instances" {
			m.editing, m.draft = true, []rune(m.search)
			if m.section == "history" && m.id != "" {
				m.draft = []rune(m.hsearch)
			}
		}
	case "s":
		if m.id != "" && m.section == "history" {
			m.hstate = cycle([]string{"", "pending", "firing", "resolved", "error", "acknowledged"}, m.hstate)
			m.page = 1
			m.events, m.event = nil, ""
			m.registerHistory()
		} else if m.id == "" || m.section == "instances" {
			m.state = cycle(states, m.state)
		}
		m.offset = 0
	case "v":
		if m.id != "" && m.section == "history" {
			m.hseverity = cycle(severities, m.hseverity)
			m.page = 1
			m.events, m.event = nil, ""
			m.registerHistory()
		} else if m.id == "" {
			m.severity = cycle(severities, m.severity)
		}
		m.offset = 0
	case "c":
		m.search, m.state, m.severity, m.group, m.page, m.offset = "", "", "", "", 1, 0
		m.hsearch, m.hstate, m.hseverity = "", "", ""
		if m.id == "" {
			m.rules, m.selected = nil, ""
			m.registerList()
		} else {
			m.events, m.event = nil, ""
			m.registerHistory()
		}
	case "d", "i", "h", "l":
		if m.id != "" {
			m.section = map[string]string{"d": "definition", "i": "instances", "h": "history", "l": "links"}[k.String()]
			m.offset = 0
		}
	case "g":
		if m.id != "" {
			if m.group != "" {
				m.group = ""
			} else {
				m.group = m.instance
			}
			m.section, m.page, m.offset = "history", 1, 0
			m.registerHistory()
		}
	case "[", "]":
		if m.id == "" || m.section == "history" {
			page := m.page
			if k.String() == "[" {
				page = max(1, page-1)
			} else if page*pageSize < m.total {
				page++
			}
			if page != m.page {
				m.page, m.offset = page, 0
				if m.id == "" {
					m.rules = nil
					m.selected = ""
					m.registerList()
				} else {
					m.events = nil
					m.event = ""
					m.registerHistory()
				}
			}
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-max(1, m.height-8))
	case "pgdown":
		m.move(max(1, m.height-8))
	case "home":
		m.move(-1000000)
	case "end":
		m.move(1000000)
	case "enter":
		if m.id == "" && m.selected != "" {
			id := m.selected
			return func() tea.Msg {
				return protocol.ShowDetail{New: func(env protocol.Env) tea.Model { return newDetail(env, id) }, Return: protocol.ReturnContext{Tab: protocol.Alerts, Selection: id}}
			}
		}
		if m.section == "links" {
			return m.navigate()
		}
		if m.section == "instances" && m.instance != "" {
			return m.inspect("instance")
		}
		if m.section == "history" && m.event != "" {
			return m.inspect("event")
		}
	case "t":
		if m.id != "" {
			m.section, m.offset = "links", 0
			m.reconcile()
			return m.navigate()
		}
	}
	m.reconcile()
	return nil
}

// Full inspections are real shell panes. Escape therefore pops one view and
// restores the parent model's independent selection, filters and page. Each
// pane owns its subscriptions and continues receiving results while hidden.
func (m *model) inspect(section string) tea.Cmd {
	id, instance, event, page := m.id, m.instance, m.event, m.page
	group, search, state := m.group, m.search, m.state
	hsearch, hstate, hseverity := m.hsearch, m.hstate, m.hseverity
	selection := instance
	if section == "event" {
		selection = event
	}
	return func() tea.Msg {
		return protocol.ShowDetail{
			New: func(env protocol.Env) tea.Model {
				n := newModel(env, id)
				n.section, n.instance, n.event, n.page = section, instance, event, page
				n.group, n.search, n.state = group, search, state
				n.hsearch, n.hstate, n.hseverity = hsearch, hstate, hseverity
				n.registerRule()
				n.registerHistory()
				return n
			},
			Return: protocol.ReturnContext{Tab: protocol.Alerts, Selection: selection},
		}
	}
}
func cycle(values []string, value string) string {
	for i, v := range values {
		if v == value {
			return values[(i+1)%len(values)]
		}
	}
	return values[0]
}
func (m *model) visibleRules() []apigen.AlertRule {
	var rows []apigen.AlertRule
	for _, r := range m.rules {
		if (m.state == "" || ruleState(r) == m.state) && (m.severity == "" || string(r.Severity) == m.severity) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m *model) instances() []apigen.AlertInstance {
	var rows []apigen.AlertInstance
	for _, raw := range m.rule.Instances {
		r := apigen.AlertInstance(raw)
		if (m.state == "" || instanceState(r) == m.state) && strings.Contains(strings.ToLower(r.GroupKey+" "+r.LabelsJson+" "+r.LastError), strings.ToLower(m.search)) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m *model) ids() ([]string, *string) {
	var ids []string
	if m.id == "" {
		for _, r := range m.visibleRules() {
			ids = append(ids, r.Id)
		}
		return ids, &m.selected
	}
	switch m.section {
	case "instances":
		for _, r := range m.instances() {
			ids = append(ids, r.GroupKey)
		}
		return ids, &m.instance
	case "history":
		for _, r := range m.events {
			ids = append(ids, r.Id)
		}
		return ids, &m.event
	case "links":
		for _, r := range m.links() {
			ids = append(ids, r.key())
		}
		return ids, &m.link
	}
	return nil, nil
}
func (m *model) reconcile() {
	ids, selected := m.ids()
	if selected == nil {
		return
	}
	if len(ids) == 0 {
		*selected = ""
		return
	}
	for _, id := range ids {
		if id == *selected {
			return
		}
	}
	*selected = ids[0]
}
func (m *model) move(delta int) {
	ids, selected := m.ids()
	if selected == nil {
		m.offset = max(0, m.offset+delta)
		return
	}
	index := 0
	for i, id := range ids {
		if id == *selected {
			index = i
			break
		}
	}
	if len(ids) > 0 {
		*selected = ids[min(len(ids)-1, max(0, index+delta))]
	}
}
func instanceState(r apigen.AlertInstance) string {
	if r.AcknowledgedAt != nil && (r.State == "firing" || r.State == "pending") {
		return "acknowledged"
	}
	return oneLine(r.State)
}
func ruleState(r apigen.AlertRule) string {
	if !r.Enabled {
		return "disabled"
	}
	for _, state := range []string{"error", "firing", "pending", "acknowledged"} {
		for _, raw := range r.Instances {
			if instanceState(apigen.AlertInstance(raw)) == state {
				return state
			}
		}
	}
	if r.LastError != "" {
		return "error"
	}
	return "resolved"
}
func (m *model) deleted() bool {
	var e *client.HTTPError
	return errors.As(m.loads["rule"].err, &e) && e.Status == 404
}
func value(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.6g", *v)
}
