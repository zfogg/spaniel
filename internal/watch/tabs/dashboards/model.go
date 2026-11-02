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

	"github.com/charmbracelet/bubbles/key"
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
	inspect                         bool
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

// ShortHelp is structural until the coordinator connects focused feature help
// to the shared footer. Keep shortcuts separate from status/data in View.
func (m *model) ShortHelp() []key.Binding {
	binding := func(keys []string, label, description string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, description))
	}
	if m.editing {
		help := []key.Binding{
			binding([]string{"enter"}, "Enter", "apply value"),
			binding([]string{"esc"}, "Esc", "cancel input"),
			binding([]string{"up", "down", "pgup", "pgdown"}, "↑/↓", "scroll input"),
		}
		if m.draft != "" {
			help = append(help, binding([]string{"backspace", "ctrl+h"}, "⌫", "delete"), binding([]string{"ctrl+u"}, "Ctrl+U", "clear"))
		}
		return help
	}
	if m.detailID == "" {
		if len(m.rows) == 0 {
			return nil
		}
		help := []key.Binding{}
		if len(m.rows) > 1 {
			help = append(help, binding([]string{"up", "down", "k", "j"}, "↑/↓", "select dashboard"))
		}
		return append(help, binding([]string{"enter"}, "Enter", "open panels"))
	}
	d := m.dashboard()
	if d == nil || m.removed {
		return nil
	}
	help := []key.Binding{}
	if len(d.Panels) > 1 {
		help = append(help, binding([]string{"left", "right", "h", "l"}, "←/→", "panel"))
	}
	if len(d.Panels) > 0 {
		mode := "live 15m window"
		if m.live {
			mode = "saved SQL/window"
		}
		help = append(help,
			binding([]string{"up", "down", "k", "j", "pgup", "pgdown", "home"}, "↑/↓ PgUp/PgDn", "scroll"),
			binding([]string{"t"}, "t", mode),
		)
		inspection := "SQL/data"
		if m.inspect {
			inspection = "visualization"
		}
		help = append(help, binding([]string{"i"}, "i", inspection))
	}
	if len(d.Variables) > 0 {
		if len(d.Variables) > 1 {
			help = append(help, binding([]string{"v"}, "v", "next variable"))
		}
		help = append(help, binding([]string{"e"}, "e", "edit $"+d.Variables[m.variable].Name))
		if len(m.overrides) > 0 {
			help = append(help, binding([]string{"r"}, "r", "restore defaults"))
		}
	}
	return help
}

func inlineHelp(bindings []key.Binding) string {
	labels := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		labels = append(labels, help.Key+" "+help.Desc)
	}
	return strings.Join(labels, " · ")
}
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
		if strings.Contains(sql, "$session_id") && m.session == "" {
			if !m.activeLoaded {
				continue
			}
			if m.activeID == "" {
				state.err = fmt.Errorf("No selected or active session for $session_id")
				continue
			}
		}
		body := apigen.PreviewDashboardQueryJSONBody{QuerySql: sql, DisplayType: &display, Variables: &values}
		m.env.Refresh.Register(client.Spec{Key: client.Key{Owner: m.env.Owner, Name: name}, Resources: []liveevent.Resource{"dashboard-panel"}, Interval: 5 * time.Second, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			scoped := body
			if session != "" {
				scoped.SessionId = &session
			}
			return api.Preview(ctx, id, scoped)
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
			for _, row := range m.rows {
				if row.Id == v.Selection {
					m.selected = v.Selection
					break
				}
			}
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
				m.scroll = 0
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
		case "i":
			if len(d.Panels) > 0 {
				m.inspect = !m.inspect
				m.scroll = 0
			}
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
	case "up":
		m.scroll = max(0, m.scroll-1)
	case "down":
		m.scroll++
	case "pgup":
		m.scroll = max(0, m.scroll-max(1, m.height/2))
	case "pgdown":
		m.scroll += max(1, m.height/2)
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

// Telemetry/definitions are untrusted terminal text; preserve no control codes.
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
