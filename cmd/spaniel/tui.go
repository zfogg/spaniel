package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/tui"
)

// tuiSubcommand returns `spaniel tui` — the mux dashboard. One WS connection
// is fanned out to a watch tab, a logs tab, and an issues tab. Tab/Shift-Tab
// cycles focus. Each sub-model keeps accumulating in the background so
// switching back to a tab shows everything it would have seen.
func tuiSubcommand() *cobra.Command {
	var apiBase string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "All-in-one dashboard: watch + logs + issues sharing one stream",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal(os.Stdout) {
				return fmt.Errorf("spaniel tui requires a terminal")
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return muxTUI(ctx, apiBase)
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.SilenceUsage = true
	return cmd
}

func muxTUI(ctx context.Context, apiBase string) error {
	ch, errCh, err := streamWS(ctx, apiBase)
	if err != nil {
		return err
	}
	m := newMuxModel(apiBase)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	go pumpStream(p, ch, errCh)
	_, runErr := p.Run()
	return runErr
}

type tabID int

const (
	tabWatch tabID = iota
	tabLogs
	tabIssues
)

type muxModel struct {
	apiBase string
	active  tabID
	width   int
	height  int

	watch  watchModel
	logs   logsModel
	issues watchModel // same model, pre-filtered to N+1 only

	connected bool
	streamErr error
}

func newMuxModel(apiBase string) muxModel {
	return muxModel{
		apiBase:   apiBase,
		watch:     newWatchModel(apiBase, spanFilter{}),
		logs:      newLogsModel(logFilter{}),
		issues:    newWatchModel(apiBase, spanFilter{N1Only: true}),
		connected: true,
	}
}

func (m muxModel) Init() tea.Cmd {
	return tea.Batch(m.watch.Init(), m.logs.Init(), m.issues.Init())
}

func (m muxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Reserve 2 lines for the tab bar + footer; sub-models render in the rest.
		sub := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 2}
		var cmds []tea.Cmd
		var c tea.Cmd
		m.watch, c = updateWatch(m.watch, sub)
		cmds = append(cmds, c)
		m.logs, c = updateLogs(m.logs, sub)
		cmds = append(cmds, c)
		m.issues, c = updateWatch(m.issues, sub)
		cmds = append(cmds, c)
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, tui.Keys.Tab):
			m.active = tabID((int(m.active) + 1) % 3)
			return m, nil
		case key.Matches(msg, tui.Keys.ShiftTab):
			m.active = tabID((int(m.active) + 2) % 3)
			return m, nil
		}
		// Forward to the focused sub-model.
		return m.forward(msg)

	case streamMsg:
		// Fan to all three so background tabs accumulate.
		var cmds []tea.Cmd
		var c tea.Cmd
		m.watch, c = updateWatch(m.watch, msg)
		cmds = append(cmds, c)
		m.logs, c = updateLogs(m.logs, msg)
		cmds = append(cmds, c)
		m.issues, c = updateWatch(m.issues, msg)
		cmds = append(cmds, c)
		return m, tea.Batch(cmds...)

	case streamEndMsg:
		m.connected = false
		m.streamErr = msg.err
		return m, nil
	}
	return m.forward(msg)
}

func (m muxModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.active {
	case tabWatch:
		m.watch, cmd = updateWatch(m.watch, msg)
	case tabLogs:
		m.logs, cmd = updateLogs(m.logs, msg)
	case tabIssues:
		m.issues, cmd = updateWatch(m.issues, msg)
	}
	return m, cmd
}

// updateWatch / updateLogs are typed wrappers around the sub-model Update so
// the cast back to the concrete type stays in one place.
func updateWatch(m watchModel, msg tea.Msg) (watchModel, tea.Cmd) {
	im, cmd := m.Update(msg)
	return im.(watchModel), cmd
}

func updateLogs(m logsModel, msg tea.Msg) (logsModel, tea.Cmd) {
	im, cmd := m.Update(msg)
	return im.(logsModel), cmd
}

func (m muxModel) View() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	tabs := []string{
		m.tabLabel("watch", tabWatch, m.watch.spansSeen),
		m.tabLabel("logs", tabLogs, m.logs.totalSeen),
		m.tabLabel("issues", tabIssues, m.issues.issuesSeen),
	}
	right := ""
	if !m.connected {
		right = tui.Danger.Render("disconnected")
	}
	tabBar := tui.HeaderBar("spaniel tui · "+strings.Join(tabs, " "), []string{right}, w)

	var body string
	switch m.active {
	case tabWatch:
		body = m.watch.View()
	case tabLogs:
		body = m.logs.View()
	case tabIssues:
		body = m.issues.View()
	}

	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Tab, tui.Keys.ShiftTab, tui.Keys.Filter, tui.Keys.Pause, tui.Keys.Quit,
	}, w)
	return tui.JoinVertical(tabBar, body, footer)
}

func (m muxModel) tabLabel(name string, id tabID, count int) string {
	label := fmt.Sprintf("%s(%d)", name, count)
	if id == m.active {
		return tui.Title.Render("[" + label + "]")
	}
	return tui.Muted.Render(" " + label + " ")
}
