package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zfogg/spaniel/internal/tui"
)

// watchTUI runs the live span-table TUI. Returns when the user quits or the
// WS stream errors. Non-TTY callers should use runWatch instead — this
// program owns the terminal.
func watchTUI(ctx context.Context, apiBase string, filter spanFilter) error {
	ch, errCh, err := streamWS(ctx, apiBase)
	if err != nil {
		return err
	}
	m := newWatchModel(apiBase, filter)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	go pumpStream(p, ch, errCh)
	_, runErr := p.Run()
	return runErr
}

// streamMsg wraps a streamEvent so it can be delivered via tea.Cmd.
type streamMsg streamEvent
type streamEndMsg struct{ err error }
type tickMsg time.Time

// pumpStream forwards every WS event into the Bubble Tea program as a
// streamMsg, then sends streamEndMsg when the channel closes.
func pumpStream(p *tea.Program, ch <-chan streamEvent, errCh <-chan error) {
	for ev := range ch {
		p.Send(streamMsg(ev))
	}
	select {
	case e := <-errCh:
		p.Send(streamEndMsg{err: e})
	default:
		p.Send(streamEndMsg{})
	}
}

// watchModel is the Bubble Tea model for `spaniel watch`. The rows table
// holds spans + issues interleaved by arrival time; the user can pause
// the feed, filter by substring, or hit Enter on a row to print the
// trace id for follow-up (`spaniel trace <id>`).
type watchModel struct {
	apiBase string
	filter  spanFilter

	table       table.Model
	input       textinput.Model
	filtering   bool
	substr      string
	paused      bool
	connected   bool
	streamErr   error
	width       int
	height      int

	// counters in the header
	spansSeen  int
	issuesSeen int
	errorsSeen int

	// the latest seen rate from streamEvent type=throughput, if any.
	spansPerSec float64
}

func newWatchModel(apiBase string, filter spanFilter) watchModel {
	cols := []table.Column{
		{Title: "time", Width: 8},
		{Title: "service", Width: 16},
		{Title: "name", Width: 40},
		{Title: "dur", Width: 8},
		{Title: "status", Width: 7},
		{Title: "trace", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true)
	styles.Selected = tui.SelectedRow
	t.SetStyles(styles)

	ti := textinput.New()
	ti.Placeholder = "filter substring…"
	ti.CharLimit = 64

	return watchModel{
		apiBase:   apiBase,
		filter:    filter,
		table:     t,
		input:     ti,
		connected: true,
	}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// 3 lines: header, footer, optional filter input
		m.table.SetHeight(max(5, msg.Height-4))
		// Re-flow the "name" column to use available width.
		m.resizeColumns()
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch {
			case key.Matches(msg, tui.Keys.Enter):
				m.substr = m.input.Value()
				m.filtering = false
				return m, nil
			case msg.Type == tea.KeyEsc:
				m.filtering = false
				m.input.SetValue("")
				m.substr = ""
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch {
		case key.Matches(msg, tui.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, tui.Keys.Pause):
			m.paused = !m.paused
			return m, nil
		case key.Matches(msg, tui.Keys.Clear):
			m.table.SetRows(nil)
			return m, nil
		case key.Matches(msg, tui.Keys.Filter):
			m.filtering = true
			m.input.Focus()
			return m, textinput.Blink
		}

	case streamMsg:
		if m.paused {
			return m, nil
		}
		m.handleEvent(streamEvent(msg))
		return m, nil

	case streamEndMsg:
		m.connected = false
		m.streamErr = msg.err
		return m, nil

	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *watchModel) resizeColumns() {
	// time(8) service(16) name(*) dur(8) status(7) trace(10) + 5 gutters
	const fixed = 8 + 16 + 8 + 7 + 10 + 5*2
	nameW := m.width - fixed
	if nameW < 10 {
		nameW = 10
	}
	cols := m.table.Columns()
	cols[2].Width = nameW
	m.table.SetColumns(cols)
}

func (m *watchModel) handleEvent(ev streamEvent) {
	switch ev.Type {
	case "span":
		var s spanEntry
		if err := json.Unmarshal(ev.Payload, &s); err != nil {
			return
		}
		if !m.filter.matchSpan(&s) {
			return
		}
		if !m.substrMatch(s.ServiceName + " " + s.Name) {
			return
		}
		m.spansSeen++
		if s.StatusCode >= 2 {
			m.errorsSeen++
		}
		status := tui.Ok.Render("ok")
		if s.StatusCode >= 2 {
			status = tui.Danger.Render("error")
		}
		short := s.TraceID
		if len(short) > 8 {
			short = short[:8]
		}
		m.appendRow(table.Row{
			time.Now().Format("15:04:05"),
			s.ServiceName,
			truncate(s.Name, m.nameWidth()),
			humanNs(s.DurationNs),
			status,
			short,
		})

	case "issue":
		var iss issueEntry
		if err := json.Unmarshal(ev.Payload, &iss); err != nil {
			return
		}
		if !m.filter.matchIssue(&iss) {
			return
		}
		m.issuesSeen++
		short := iss.TraceID
		if len(short) > 8 {
			short = short[:8]
		}
		label := tui.Warn.Render("[" + issueKindLabel(iss.Kind) + "]")
		m.appendRow(table.Row{
			time.Now().Format("15:04:05"),
			"—",
			fmt.Sprintf("%s count=%d wasted=%s", label, iss.Count, humanNs(iss.WastedNs)),
			"—",
			tui.Warn.Render("issue"),
			short,
		})

	case "throughput":
		var p struct {
			SpansPerSec float64 `json:"spansPerSec"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		m.spansPerSec = p.SpansPerSec
	}
}

func (m watchModel) substrMatch(s string) bool {
	if m.substr == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(m.substr))
}

func (m *watchModel) appendRow(r table.Row) {
	rows := m.table.Rows()
	// Keep the most recent 1000 rows to bound memory on long sessions.
	const cap = 1000
	if len(rows) >= cap {
		rows = rows[len(rows)-cap+1:]
	}
	rows = append(rows, r)
	m.table.SetRows(rows)
	// Auto-scroll to the bottom unless the user has paged up.
	m.table.GotoBottom()
}

func (m watchModel) nameWidth() int {
	cols := m.table.Columns()
	if len(cols) < 3 {
		return 40
	}
	return cols[2].Width
}

func (m watchModel) View() string {
	pills := []string{
		fmt.Sprintf("spans %d", m.spansSeen),
		fmt.Sprintf("errors %d", m.errorsSeen),
		fmt.Sprintf("issues %d", m.issuesSeen),
	}
	if m.spansPerSec > 0 {
		pills = append(pills, fmt.Sprintf("%.1f/s", m.spansPerSec))
	}
	if m.filter.Service != "" {
		pills = append(pills, "svc="+m.filter.Service)
	}
	if m.filter.ErrorsOnly {
		pills = append(pills, "errors-only")
	}
	if m.filter.N1Only {
		pills = append(pills, "n+1-only")
	}
	if m.substr != "" {
		pills = append(pills, "/"+m.substr)
	}
	if m.paused {
		pills = append(pills, tui.Warn.Render("PAUSED"))
	}
	if !m.connected {
		pills = append(pills, tui.Danger.Render("disconnected"))
	}

	header := tui.HeaderBar("spaniel watch", pills, m.widthOr80())
	body := m.table.View()
	if m.filtering {
		body = m.input.View() + "\n" + body
	}
	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Up, tui.Keys.Down, tui.Keys.Filter, tui.Keys.Pause, tui.Keys.Clear, tui.Keys.Quit,
	}, m.widthOr80())

	if m.streamErr != nil {
		body = lipgloss.JoinVertical(lipgloss.Left, body, tui.Toast.Render("stream error: "+m.streamErr.Error()))
	}
	return tui.JoinVertical(header, body, footer)
}

func (m watchModel) widthOr80() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

// truncate clips s to n display columns, appending "…" if it had to cut.
func truncate(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
