package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/tui"
)

// sessionListModel is the interactive picker. Enter activates the focused
// session, d marks it for deletion (with a confirmation step), q quits.
type sessionListModel struct {
	apiBase  string
	sessions []storage.Session
	activeID string

	table   table.Model
	confirm bool   // d pressed once → waiting for second confirmation
	status  string // last action's outcome, rendered in the footer area

	width  int
	height int
}

func runSessionListTUI(sessions []storage.Session, activeID, apiBase string) error {
	m := newSessionListModel(sessions, activeID, apiBase)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newSessionListModel(sessions []storage.Session, activeID, apiBase string) sessionListModel {
	cols := []table.Column{
		{Title: "", Width: 1},
		{Title: "id", Width: 11},
		{Title: "label", Width: 28},
		{Title: "flags", Width: 18},
		{Title: "traces", Width: 6},
		{Title: "spans", Width: 6},
		{Title: "age", Width: 6},
	}
	tbl := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true)
	st.Selected = tui.SelectedRow
	tbl.SetStyles(st)
	tbl.SetRows(buildSessionRows(sessions, activeID))

	return sessionListModel{
		apiBase:  apiBase,
		sessions: sessions,
		activeID: activeID,
		table:    tbl,
	}
}

func buildSessionRows(sessions []storage.Session, activeID string) []table.Row {
	now := time.Now()
	rows := make([]table.Row, 0, len(sessions))
	for _, s := range sessions {
		marker := " "
		if s.ID == activeID {
			marker = tui.Accent.Render("*")
		}
		var flags []string
		if s.IsBaseline {
			flags = append(flags, "baseline")
		}
		if s.IsImported {
			flags = append(flags, "imported")
		}
		flagStr := strings.Join(flags, ",")
		if flagStr == "" {
			flagStr = "-"
		}
		age := now.Sub(time.Unix(0, s.CreatedAt))
		rows = append(rows, table.Row{
			marker,
			shortID(s.ID),
			s.Label,
			flagStr,
			fmt.Sprint(s.TraceCount),
			fmt.Sprint(s.SpanCount),
			fmtAge(age),
		})
	}
	return rows
}

func (m sessionListModel) Init() tea.Cmd { return nil }

func (m sessionListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetHeight(max(5, m.height-4))
		return m, nil
	case tea.KeyMsg:
		if m.confirm {
			switch msg.String() {
			case "y", "Y":
				return m, m.doDelete()
			default:
				m.confirm = false
				m.status = ""
				return m, nil
			}
		}
		switch {
		case key.Matches(msg, tui.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, tui.Keys.Enter):
			return m, m.doActivate()
		case msg.String() == "b":
			return m, m.doBaseline()
		case msg.String() == "d":
			if len(m.sessions) == 0 {
				return m, nil
			}
			s := m.selected()
			if s.ID == m.activeID {
				m.status = tui.Warn.Render("cannot delete the active session")
				return m, nil
			}
			m.confirm = true
			m.status = tui.Warn.Render(fmt.Sprintf("delete %s? (y/N)", s.Label))
			return m, nil
		}
	case sessionRefreshMsg:
		m.sessions = msg.sessions
		m.activeID = msg.activeID
		m.table.SetRows(buildSessionRows(m.sessions, m.activeID))
		if msg.status != "" {
			m.status = msg.status
		}
		m.confirm = false
		return m, nil
	case sessionErrMsg:
		m.status = tui.Danger.Render(msg.err.Error())
		m.confirm = false
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m sessionListModel) selected() storage.Session {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.sessions) {
		return storage.Session{}
	}
	return m.sessions[idx]
}

func (m sessionListModel) doActivate() tea.Cmd {
	s := m.selected()
	if s.ID == "" {
		return nil
	}
	apiBase := m.apiBase
	return func() tea.Msg {
		resp, err := http.Post(apiBase+"/api/sessions/"+s.ID+"/activate", "application/json", nil) //nolint:gosec
		if err != nil {
			return sessionErrMsg{err: err}
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			return sessionErrMsg{err: fmt.Errorf("activate failed: HTTP %d", resp.StatusCode)}
		}
		return refreshAfter(apiBase, fmt.Sprintf("activated %s", s.Label))
	}
}

func (m sessionListModel) doBaseline() tea.Cmd {
	s := m.selected()
	if s.ID == "" {
		return nil
	}
	apiBase := m.apiBase
	return func() tea.Msg {
		body := strings.NewReader(fmt.Sprintf(`{"is_baseline":%t}`, !s.IsBaseline))
		resp, err := http.Post(apiBase+"/api/sessions/"+s.ID+"/baseline", "application/json", body) //nolint:gosec
		if err != nil {
			return sessionErrMsg{err: err}
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			return sessionErrMsg{err: fmt.Errorf("baseline toggle failed: HTTP %d", resp.StatusCode)}
		}
		verb := "marked baseline"
		if s.IsBaseline {
			verb = "unmarked baseline"
		}
		return refreshAfter(apiBase, fmt.Sprintf("%s: %s", verb, s.Label))
	}
}

func (m sessionListModel) doDelete() tea.Cmd {
	s := m.selected()
	if s.ID == "" {
		return nil
	}
	apiBase := m.apiBase
	return func() tea.Msg {
		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/api/sessions/"+s.ID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return sessionErrMsg{err: err}
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			return sessionErrMsg{err: fmt.Errorf("delete failed: HTTP %d", resp.StatusCode)}
		}
		return refreshAfter(apiBase, fmt.Sprintf("deleted %s", s.Label))
	}
}

// refreshAfter re-fetches the session list and returns a refresh msg with
// the supplied status string attached.
func refreshAfter(apiBase, status string) tea.Msg {
	sessions, activeID, err := sessionListData(apiBase)
	if err != nil {
		return sessionErrMsg{err: err}
	}
	return sessionRefreshMsg{sessions: sessions, activeID: activeID, status: tui.Ok.Render(status)}
}

type sessionRefreshMsg struct {
	sessions []storage.Session
	activeID string
	status   string
}

type sessionErrMsg struct{ err error }

func (m sessionListModel) View() string {
	w := m.widthOr(100)
	header := tui.HeaderBar("spaniel session list", []string{fmt.Sprintf("%d sessions", len(m.sessions))}, w)
	body := m.table.View()
	if m.status != "" {
		body = m.status + "\n" + body
	}
	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Enter,
		key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "baseline")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		tui.Keys.Up, tui.Keys.Down, tui.Keys.Quit,
	}, w)
	return tui.JoinVertical(header, body, footer)
}

func (m sessionListModel) widthOr(d int) int {
	if m.width > 0 {
		return m.width
	}
	return d
}
