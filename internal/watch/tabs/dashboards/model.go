// Package dashboards implements the read-only saved dashboard browser.
package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

type panelState struct {
	result          apigen.QueryPreview
	err             error
	loaded, pending bool
	sequence        uint64
}
type model struct {
	definitionStatus                string
	definitionPending               bool
	env                             protocol.Env
	rows                            []apigen.Dashboard
	selected, detailID, panelID     string
	loaded, removed, live, editing  bool
	err                             error
	connection                      protocol.Connection
	session, activeID               string
	activeLoaded                    bool
	width, height, scroll, variable int
	draft, inputError               string
	overrides                       map[string]string
	panels                          map[string]*panelState
	generation                      uint64
	listSequence, activeSequence    uint64
}

// New is the stable shell constructor. Definitions are read from the hydrated
// list: the single-dashboard GET marks definitions seen and is not read-only.
func New(env protocol.Env) tea.Model { return newModel(env, "") }
func newModel(env protocol.Env, id string) *model {
	m := &model{env: env, detailID: id, width: 80, height: 20, overrides: map[string]string{}, panels: map[string]*panelState{}}
	env.Refresh.Register(client.Spec{Key: client.Key{Owner: env.Owner, Name: "list"}, Resources: []liveevent.Resource{"dashboards"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Dashboards(ctx, client.Scope(url.Values{}, session))
	}})
	if id != "" {
		m.registerActive()
	}
	return m
}
func (m *model) registerActive() {
	m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: "active"}, Resources: []liveevent.Resource{"active-session"}, Fetch: func(ctx context.Context, api *client.Client, _ string) (any, error) {
		return api.ActiveSession(ctx, url.Values{})
	}})
}
func (m *model) Init() tea.Cmd      { return nil }
func (m *model) CapturesKeys() bool { return m.editing }
func (m *model) dashboard() *apigen.Dashboard {
	for i := range m.rows {
		if m.rows[i].Id == m.detailID {
			return &m.rows[i]
		}
	}
	return nil
}
func (m *model) selectedIndex() int {
	for i, d := range m.rows {
		if d.Id == m.selected {
			return i
		}
	}
	return 0
}
func (m *model) panelIndex() int {
	if d := m.dashboard(); d != nil {
		for i, p := range d.Panels {
			if p.Id == m.panelID {
				return i
			}
		}
	}
	return 0
}
func (m *model) cancelPanels() {
	for name := range m.panels {
		m.env.Refresh.Remove(client.Key{Owner: m.env.Owner, Name: name})
	}
	m.panels = map[string]*panelState{}
	m.generation++
}

var liveClock = regexp.MustCompile(`(?s)coalesce\(greatest\(\(SELECT max\(timestamp_ns\).*?epoch_ns\(current_timestamp\)\)`)

func panelSQL(sql string, live bool) string {
	if live {
		return liveClock.ReplaceAllString(sql, "epoch_ns(current_timestamp)")
	}
	return sql
}
func (m *model) queryPanels() {
	m.cancelPanels()
	d := m.dashboard()
	if d == nil {
		return
	}
	for _, p := range d.Panels {
		name := fmt.Sprintf("panel:%d:%s", m.generation, p.Id)
		state := &panelState{}
		m.panels[name] = state
		sql := panelSQL(p.QuerySql, m.live)
		display := string(p.DisplayType)
		id := d.Id
		values := map[string]string{}
		for _, v := range d.Variables {
			values[v.Name] = v.DefaultValue
		}
		for k, v := range m.overrides {
			values[k] = v
		}
		if strings.Contains(sql, "$session_id") {
			if !m.activeLoaded {
				continue
			}
			if m.session != "" && m.session != m.activeID {
				state.err = fmt.Errorf("Query preview cannot scope $session_id to %q (server active session %q). Shared API session support is required", m.session, m.activeID)
				continue
			}
			if m.activeID == "" {
				state.err = fmt.Errorf("No active session for $session_id")
				continue
			}
		}
		body := apigen.PreviewDashboardQueryJSONBody{QuerySql: sql, DisplayType: &display, Variables: &values}
		m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: name}, Resources: []liveevent.Resource{"dashboard-panel"}, Interval: 5 * time.Second, Fetch: func(ctx context.Context, api *client.Client, _ string) (any, error) {
			return api.Preview(ctx, id, body)
		}})
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
	case protocol.Live:
		// Pinned tabs still observe the server's active session. Replacing this
		// subscription cancels a snapshot that could otherwise overwrite the
		// newer frame; it does not start a second stream or poll independently.
		if event, ok := v.Event.Decoded.(*liveevent.ActiveSessionPayload); ok && m.detailID != "" && (!m.activeLoaded || event.ID != m.activeID) {
			m.activeID, m.activeLoaded = event.ID, true
			m.registerActive()
			m.queryPanels()
		}
		if event, ok := v.Event.Decoded.(*liveevent.DefinitionSyncPayload); ok && event.Kind == "dashboard" {
			if event.Status == "updated" {
				m.definitionStatus = ""
			} else {
				m.definitionStatus = "Definition sync " + event.Status + ": " + event.Message
			}
		}
	case protocol.Connection:
		m.connection = v
		if v.Resync && m.detailID != "" {
			// Revalidate the server-owned built-in before previews after a gap.
			m.activeLoaded = false
			m.queryPanels()
		}
	case protocol.SessionChanged:
		if m.session != v.ID {
			m.session = v.ID
			m.scroll = 0
			m.queryPanels()
		}
	case protocol.ReturnContext:
		if v.Tab == protocol.Dashboards {
			m.selected = v.Selection
		}
	case protocol.Result:
		if v.Key.Owner != m.env.Owner {
			return m, nil
		}
		switch v.Key.Name {
		case "list":
			if v.Sequence < m.listSequence {
				return m, nil
			}
			m.listSequence = v.Sequence
			m.definitionPending = v.Pending
			if v.Err != nil {
				m.err = v.Err
				return m, nil
			}
			data, ok := v.Data.(client.Envelope[[]apigen.Dashboard])
			if !ok {
				return m, nil
			}
			old := m.dashboard()
			var previous *apigen.Dashboard
			if old != nil {
				copy := *old
				previous = &copy
			}
			m.rows = append([]apigen.Dashboard(nil), data.Data...)
			m.loaded = true
			m.err = nil
			if len(m.rows) > 0 {
				found := false
				for _, d := range m.rows {
					found = found || d.Id == m.selected
				}
				if !found {
					m.selected = m.rows[0].Id
				}
			}
			if m.detailID != "" {
				d := m.dashboard()
				m.removed = d == nil
				if d == nil {
					m.cancelPanels()
					m.editing = false
					return m, nil
				}
				sort.SliceStable(d.Panels, func(i, j int) bool {
					if d.Panels[i].Position != d.Panels[j].Position {
						return d.Panels[i].Position < d.Panels[j].Position
					}
					if d.Panels[i].Title != d.Panels[j].Title {
						return d.Panels[i].Title < d.Panels[j].Title
					}
					return d.Panels[i].Id < d.Panels[j].Id
				})
				if !reflect.DeepEqual(previous, d) {
					valid := map[string]bool{}
					for _, x := range d.Variables {
						valid[x.Name] = true
					}
					for k := range m.overrides {
						if !valid[k] {
							delete(m.overrides, k)
						}
					}
					m.variable = min(m.variable, max(0, len(d.Variables)-1))
					m.editing = false
					found := false
					for _, p := range d.Panels {
						found = found || p.Id == m.panelID
					}
					if !found && len(d.Panels) > 0 {
						previousPanelID := m.panelID
						m.panelID = d.Panels[0].Id
						// File reload replaces child IDs. Preserve an unambiguous
						// panel title when the selected panel survives that reload.
						if previous != nil {
							for _, oldPanel := range previous.Panels {
								if oldPanel.Id != previousPanelID {
									continue
								}
								candidate, matches := "", 0
								for _, panel := range d.Panels {
									if panel.Title == oldPanel.Title {
										candidate, matches = panel.Id, matches+1
									}
								}
								if matches == 1 {
									m.panelID = candidate
								}
								break
							}
						}
						m.scroll = 0
					}
					m.queryPanels()
				}
			}
		case "active":
			if v.Sequence < m.activeSequence {
				return m, nil
			}
			m.activeSequence = v.Sequence
			if v.Err != nil {
				m.activeLoaded = false
				m.cancelPanels()
				m.inputError = "Active session: " + v.Err.Error()
				return m, nil
			}
			data, ok := v.Data.(client.Envelope[apigen.ActiveSession])
			if !ok {
				return m, nil
			}
			id := data.Data.Id
			changed := !m.activeLoaded || id != m.activeID
			m.activeLoaded = true
			m.activeID = id
			if changed {
				m.inputError = ""
				m.queryPanels()
			}
		default:
			s := m.panels[v.Key.Name]
			if s == nil || v.Sequence < s.sequence {
				return m, nil
			}
			s.sequence = v.Sequence
			s.err = v.Err
			s.pending = v.Pending
			if data, ok := v.Data.(client.Envelope[apigen.QueryPreview]); ok && v.Err == nil {
				s.result = data.Data
				s.loaded = true
			}
		}
	case tea.KeyMsg:
		if m.editing {
			return m, m.edit(v)
		}
		if m.detailID == "" {
			i := m.selectedIndex()
			switch v.String() {
			case "up", "k":
				i = max(0, i-1)
			case "down", "j":
				i = min(len(m.rows)-1, i+1)
			case "enter":
				if len(m.rows) == 0 {
					return m, nil
				}
				id := m.rows[i].Id
				return m, func() tea.Msg {
					return protocol.ShowDetail{New: func(env protocol.Env) tea.Model { return newModel(env, id) }, Return: protocol.ReturnContext{Tab: protocol.Dashboards, Selection: id}}
				}
			}
			if i >= 0 && i < len(m.rows) {
				m.selected = m.rows[i].Id
			}
			return m, nil
		}
		d := m.dashboard()
		if d == nil {
			return m, nil
		}
		switch v.String() {
		case "left", "h", "right", "l":
			i := m.panelIndex()
			if v.String() == "left" || v.String() == "h" {
				i = max(0, i-1)
			} else {
				i = min(len(d.Panels)-1, i+1)
			}
			if i >= 0 && i < len(d.Panels) {
				m.panelID = d.Panels[i].Id
				m.scroll = 0
			}
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "pgdown":
			m.scroll += max(1, m.height-8)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-8))
		case "home":
			m.scroll = 0
		case "v":
			if len(d.Variables) > 0 {
				m.variable = (m.variable + 1) % len(d.Variables)
			}
		case "e":
			if len(d.Variables) > 0 {
				x := d.Variables[m.variable]
				m.draft = x.DefaultValue
				if value, ok := m.overrides[x.Name]; ok {
					m.draft = value
				}
				m.editing = true
				m.inputError = ""
			}
		case "r":
			m.overrides = map[string]string{}
			m.inputError = ""
			m.queryPanels()
		case "t":
			m.live = !m.live
			m.scroll = 0
			m.queryPanels()
		}
	}
	return m, nil
}
func (m *model) edit(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.editing = false
		m.inputError = ""
	case "enter":
		d := m.dashboard()
		if d == nil || m.variable >= len(d.Variables) {
			m.editing = false
			return nil
		}
		x := d.Variables[m.variable]
		if string(x.Kind) == "enum" && m.draft != "" {
			var options []string
			if json.Unmarshal([]byte(x.OptionsJson), &options) != nil {
				m.inputError = "Invalid enum options"
				return nil
			}
			found := false
			for _, o := range options {
				found = found || o == m.draft
			}
			if !found {
				m.inputError = "Choose an enum option"
				return nil
			}
		}
		m.overrides[x.Name] = m.draft
		m.editing = false
		m.inputError = ""
		m.scroll = 0
		m.queryPanels()
	case "backspace", "ctrl+h":
		r := []rune(m.draft)
		if len(r) > 0 {
			m.draft = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.draft = ""
	default:
		if key.Type == tea.KeyRunes && len(m.draft) < 16000 {
			m.draft += string(key.Runes)
		}
	case "space":
		m.draft += " "
	}
	return nil
}
func (m *model) stateFor(id string) *panelState {
	return m.panels[fmt.Sprintf("panel:%d:%s", m.generation, id)]
}
func (m *model) View() string {
	var lines []string
	if m.definitionStatus != "" {
		lines = append(lines, m.definitionStatus)
	}
	if m.definitionPending {
		lines = append(lines, "STALE · definition changes pending repair")
	}
	if !m.connection.Connected {
		lines = append(lines, "STALE · disconnected/reconnecting; retained results")
	}
	if m.err != nil {
		lines = append(lines, "Definition error (retained data): "+m.err.Error())
	}
	if !m.loaded {
		lines = append(lines, "Loading dashboards…")
		return screen(lines, m.width, m.height)
	}
	if m.detailID == "" {
		lines = append(lines, fmt.Sprintf("%d dashboards · read-only · ↑/↓ select · Enter panels", len(m.rows)))
		if len(m.rows) == 0 {
			lines = append(lines, "No saved or file-managed dashboards.")
		}
		start := max(0, m.selectedIndex()-max(1, m.height-5)/2)
		for _, d := range m.rows[start:] {
			mark := "  "
			if d.Id == m.selected {
				mark = "› "
			}
			source := "saved"
			if d.SourceFile != "" {
				source = "file: " + d.SourceFile
			}
			lines = append(lines, fmt.Sprintf("%s%s · %d panels · %d variables · %s", mark, d.Name, len(d.Panels), len(d.Variables), source), "  "+d.Description+" · updated "+time.Unix(0, d.UpdatedAt).Format(time.RFC3339))
		}
		return screen(lines, m.width, m.height)
	}
	if m.removed {
		lines = append(lines, "Dashboard removed externally. Escape returns to dashboards.")
		return screen(lines, m.width, m.height)
	}
	d := m.dashboard()
	if d == nil {
		return screen(append(lines, "Loading definition…"), m.width, m.height)
	}
	mode := "saved SQL/window"
	if m.live {
		mode = "live SQL clock · 15m chart window"
	}
	lines = append(lines, d.Name+" · read-only · "+mode, "←/→ panel · ↑/↓ scroll · t saved/live · v variable · e value · r defaults")
	lines = append(lines, "Scope: "+m.session+" · $session_id is server active: "+m.activeID)
	if len(d.Variables) > 0 {
		x := d.Variables[m.variable]
		value := x.DefaultValue
		if o, ok := m.overrides[x.Name]; ok {
			value = o
		}
		lines = append(lines, fmt.Sprintf("$%s (%s) = %s · default %s · options %s", x.Name, x.Kind, value, x.DefaultValue, x.OptionsJson))
		if m.editing {
			lines = append(lines, "Value: "+m.draft+"█ · Enter apply · Esc cancel")
		}
	}
	if m.inputError != "" {
		lines = append(lines, m.inputError)
	}
	if len(d.Panels) == 0 {
		return screen(append(lines, "No panels in this dashboard."), m.width, m.height)
	}
	index := m.panelIndex()
	p := d.Panels[index]
	titles := []string{}
	for i, p := range d.Panels {
		label := p.Title
		s := m.stateFor(p.Id)
		if s == nil || !s.loaded {
			label += " [loading]"
		}
		if s != nil && s.err != nil {
			label = p.Title + " [error]"
		}
		if i == index {
			label = "[" + label + "]"
		}
		titles = append(titles, label)
	}
	lines = append(lines, fmt.Sprintf("Panel %d/%d · %s", index+1, len(d.Panels), strings.Join(titles, " | ")))
	body := []string{p.Title + " · type: " + string(p.DisplayType), "SQL: " + panelSQL(p.QuerySql, m.live)}
	s := m.stateFor(p.Id)
	if s == nil || (!s.loaded && s.err == nil) {
		body = append(body, "Loading query results…")
	} else {
		if s.err != nil {
			body = append(body, "Query error: "+s.err.Error())
			if s.loaded {
				body = append(body, "STALE · last successful result:")
			}
		}
		if s.pending {
			body = append(body, "STALE · durable changes pending repair")
		}
		if s.loaded {
			body = append(body, renderPanel(string(p.DisplayType), p.SettingsJson, s.result, m.width, max(4, m.height-len(lines)-5), m.live)...)
		}
	}
	for i := range lines {
		lines[i] = ansi.Truncate(safe(lines[i]), m.width, "…")
	}
	// SQL and all result cells wrap before scrolling, including narrow terminals.
	wrapped := wrapLines(body, m.width)
	available := max(1, m.height-len(lines))
	// Keep the retained offset at the visible boundary so repeated Down at the
	// end does not require equally many Up presses before content moves again.
	m.scroll = min(m.scroll, max(0, len(wrapped)-available))
	offset := m.scroll
	lines = append(lines, wrapped[offset:min(len(wrapped), offset+available)]...)
	return screen(lines, m.width, m.height)
}
func wrapLines(lines []string, width int) []string {
	var out []string
	for _, line := range lines {
		out = append(out, strings.Split(ansi.Hardwrap(safe(line), max(1, width), true), "\n")...)
	}
	return out
}
func screen(lines []string, width, height int) string {
	out := wrapLines(lines, width)
	return strings.Join(out[:min(len(out), max(1, height))], "\n")
}

// Telemetry/definitions are untrusted terminal text; preserve no control codes.
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
