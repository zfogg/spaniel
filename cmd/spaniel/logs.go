package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/tui"
)

// logEntry is the wire shape of ws.LogPayload — duplicated here so the CLI
// doesn't import internal/ws.
type logEntry struct {
	TraceID     string `json:"traceId"`
	SpanID      string `json:"spanId"`
	Severity    int    `json:"severity"`
	Body        string `json:"body"`
	ServiceName string `json:"serviceName"`
	SessionID   string `json:"sessionId"`
}

// logsSubcommand returns the `spaniel logs` group. Only `tail` exists today.
// The old non-TTY printf path was removed — `spaniel logs tail` now requires
// a terminal (use `spaniel watch | …` style piping if you need plain text).
func logsSubcommand() *cobra.Command {
	logs := &cobra.Command{
		Use:   "logs",
		Short: "Inspect logs from a running spaniel",
	}

	var (
		apiBase  string
		service  string
		traceID  string
		severity string
	)
	tail := &cobra.Command{
		Use:   "tail",
		Short: "Stream live logs in an interactive pager",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal(os.Stdout) {
				return fmt.Errorf("spaniel logs tail requires a terminal — run it interactively")
			}
			f := logFilter{
				Service:     service,
				TraceID:     traceID,
				MinSeverity: severityFromName(severity),
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return logsTailTUI(ctx, apiBase, f)
		},
	}
	tail.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	tail.Flags().StringVar(&service, "service", "", "Filter: only show logs from this service")
	tail.Flags().StringVar(&traceID, "trace", "", "Filter: only show logs from this trace id")
	tail.Flags().StringVar(&severity, "severity", "", "Filter: minimum severity (trace,debug,info,warn,error,fatal)")
	tail.SilenceUsage = true
	logs.AddCommand(tail)
	return logs
}

type logFilter struct {
	Service     string
	TraceID     string
	MinSeverity int
}

func (f logFilter) match(l *logEntry) bool {
	if f.Service != "" && l.ServiceName != f.Service {
		return false
	}
	if f.TraceID != "" && !strings.HasPrefix(l.TraceID, f.TraceID) {
		return false
	}
	if f.MinSeverity > 0 && l.Severity < f.MinSeverity {
		return false
	}
	return true
}

// logsTailTUI runs the viewport-based tail -f. Follow-mode auto-scrolls to
// the latest line; scrolling up disables follow until the user presses F.
func logsTailTUI(ctx context.Context, apiBase string, filter logFilter) error {
	ch, errCh, err := streamWS(ctx, apiBase)
	if err != nil {
		return err
	}
	m := newLogsModel(filter)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	go pumpStream(p, ch, errCh)
	_, runErr := p.Run()
	return runErr
}

type logsModel struct {
	filter logFilter
	vp     viewport.Model
	input  textinput.Model

	lines     []string // styled, ready-to-render lines
	follow    bool
	filtering bool
	substr    string
	paused    bool
	connected bool
	streamErr error

	width  int
	height int

	totalSeen   int
	warns       int
	errors      int
}

func newLogsModel(filter logFilter) logsModel {
	vp := viewport.New(80, 20)
	ti := textinput.New()
	ti.Placeholder = "filter substring…"
	ti.CharLimit = 64
	return logsModel{
		filter:    filter,
		vp:        vp,
		input:     ti,
		follow:    true,
		connected: true,
	}
}

func (m logsModel) Init() tea.Cmd { return nil }

func (m logsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		m.vp.Height = max(5, msg.Height-4)
		m.rerender()
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch {
			case key.Matches(msg, tui.Keys.Enter):
				m.substr = m.input.Value()
				m.filtering = false
				m.rerender()
				return m, nil
			case msg.Type == tea.KeyEsc:
				m.filtering = false
				m.input.SetValue("")
				m.substr = ""
				m.rerender()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch {
		case key.Matches(msg, tui.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, tui.Keys.Filter):
			m.filtering = true
			m.input.Focus()
			return m, textinput.Blink
		case key.Matches(msg, tui.Keys.Pause):
			m.paused = !m.paused
			return m, nil
		case key.Matches(msg, tui.Keys.Follow):
			m.follow = !m.follow
			if m.follow {
				m.vp.GotoBottom()
			}
			return m, nil
		case key.Matches(msg, tui.Keys.Clear):
			m.lines = nil
			m.rerender()
			return m, nil
		}
		// Manual scroll disables follow; user explicitly takes the wheel.
		m.follow = false

	case streamMsg:
		if m.paused {
			return m, nil
		}
		m.appendEvent(streamEvent(msg))
		return m, nil

	case streamEndMsg:
		m.connected = false
		m.streamErr = msg.err
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *logsModel) appendEvent(ev streamEvent) {
	if ev.Type != "log" {
		return
	}
	var l logEntry
	if err := json.Unmarshal(ev.Payload, &l); err != nil {
		return
	}
	if !m.filter.match(&l) {
		return
	}
	if m.substr != "" && !strings.Contains(strings.ToLower(l.Body+" "+l.ServiceName), strings.ToLower(m.substr)) {
		return
	}
	m.totalSeen++
	if l.Severity >= 17 {
		m.errors++
	} else if l.Severity >= 13 {
		m.warns++
	}
	m.lines = append(m.lines, styleLogLine(&l))
	// Bound memory on very long sessions.
	const cap = 5000
	if len(m.lines) > cap {
		m.lines = m.lines[len(m.lines)-cap:]
	}
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m *logsModel) rerender() {
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	if m.follow {
		m.vp.GotoBottom()
	}
}

// styleLogLine is the TUI-mode analog of the old formatLog. Returns a
// Lipgloss-styled string ready for the viewport.
func styleLogLine(l *logEntry) string {
	sev := severityName(l.Severity)
	ts := time.Now().Format("15:04:05")
	trace := ""
	if l.TraceID != "" {
		short := l.TraceID
		if len(short) > 8 {
			short = short[:8]
		}
		trace = " " + tui.Faint.Render("trace="+short)
	}
	prefix := fmt.Sprintf("%s %s %-16s%s  ", tui.Faint.Render(ts), tui.SeverityStyle(l.Severity).Render(sev), l.ServiceName, trace)
	return prefix + tui.SeverityStyle(l.Severity).Render(l.Body)
}

func (m logsModel) View() string {
	pills := []string{
		fmt.Sprintf("logs %d", m.totalSeen),
		fmt.Sprintf("warn %d", m.warns),
		fmt.Sprintf("err %d", m.errors),
	}
	if m.filter.Service != "" {
		pills = append(pills, "svc="+m.filter.Service)
	}
	if m.filter.TraceID != "" {
		pills = append(pills, "trace="+m.filter.TraceID)
	}
	if m.filter.MinSeverity > 0 {
		pills = append(pills, ">="+severityName(m.filter.MinSeverity))
	}
	if m.substr != "" {
		pills = append(pills, "/"+m.substr)
	}
	if m.follow {
		pills = append(pills, tui.Ok.Render("FOLLOW"))
	}
	if m.paused {
		pills = append(pills, tui.Warn.Render("PAUSED"))
	}
	if !m.connected {
		pills = append(pills, tui.Danger.Render("disconnected"))
	}

	w := m.widthOr80()
	header := tui.HeaderBar("spaniel logs tail", pills, w)
	body := m.vp.View()
	if m.filtering {
		body = m.input.View() + "\n" + body
	}
	if m.streamErr != nil {
		body = lipgloss.JoinVertical(lipgloss.Left, body, tui.Toast.Render("stream error: "+m.streamErr.Error()))
	}
	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Filter, tui.Keys.Follow, tui.Keys.Pause, tui.Keys.Clear, tui.Keys.Quit,
	}, w)
	return tui.JoinVertical(header, body, footer)
}

func (m logsModel) widthOr80() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}
