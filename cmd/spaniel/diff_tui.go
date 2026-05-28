package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/tui"
)

// diffTUI launches the interactive diff pager. Layout: header (sessions +
// summary), table (left), detail pane (right), footer.
func diffTUI(r api.DiffResult) error {
	m := newDiffModel(r)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type diffModel struct {
	result api.DiffResult
	rows   []api.DiffSpan // table-ordered: regressed first, then added, then removed

	table  table.Model
	detail viewport.Model

	width  int
	height int
}

func newDiffModel(r api.DiffResult) diffModel {
	// Build the row order so the most interesting things sort to the top.
	regressed := sortRegressedDesc(r.Spans)
	var added, removed []api.DiffSpan
	for _, s := range r.Spans {
		switch s.Status {
		case "added":
			added = append(added, s)
		case "removed":
			removed = append(removed, s)
		}
	}
	rows := append([]api.DiffSpan(nil), regressed...)
	rows = append(rows, added...)
	rows = append(rows, removed...)

	cols := []table.Column{
		{Title: "status", Width: 9},
		{Title: "service", Width: 18},
		{Title: "name", Width: 40},
		{Title: "baseline", Width: 10},
		{Title: "compare", Width: 10},
		{Title: "Δ", Width: 8},
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

	trows := make([]table.Row, 0, len(rows))
	for _, s := range rows {
		trows = append(trows, diffRow(s))
	}
	tbl.SetRows(trows)

	return diffModel{
		result: r,
		rows:   rows,
		table:  tbl,
		detail: viewport.New(40, 20),
	}
}

func diffRow(s api.DiffSpan) table.Row {
	var status string
	switch s.Status {
	case "changed":
		if s.DeltaPct > 0 {
			status = tui.Danger.Render("regress")
		} else {
			status = tui.Ok.Render("faster")
		}
	case "added":
		status = tui.Warn.Render("added")
	case "removed":
		status = tui.Muted.Render("removed")
	default:
		status = s.Status
	}
	return table.Row{
		status,
		s.ServiceName,
		s.Name,
		fmtNs(s.BaselineDurationNs),
		fmtNs(s.CompareDurationNs),
		pctStr(s.DeltaPct),
	}
}

func (m diffModel) Init() tea.Cmd { return nil }

func (m diffModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		rightW := m.width / 3
		if rightW < 30 {
			rightW = 30
		}
		m.detail.Width = rightW
		m.detail.Height = max(5, m.height-4)
		m.table.SetHeight(max(5, m.height-4))
		m.refreshDetail()
		return m, nil
	case tea.KeyMsg:
		if key.Matches(msg, tui.Keys.Quit) {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	m.refreshDetail()
	return m, cmd
}

func (m *diffModel) refreshDetail() {
	if len(m.rows) == 0 {
		m.detail.SetContent(tui.Muted.Render("no changed spans"))
		return
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		idx = 0
	}
	s := m.rows[idx]
	lines := []string{
		tui.Title.Render(s.ServiceName + " · " + s.Name),
		"",
		tui.Muted.Render("status:   ") + s.Status,
		tui.Muted.Render("baseline: ") + fmtNs(s.BaselineDurationNs),
		tui.Muted.Render("compare:  ") + fmtNs(s.CompareDurationNs),
		tui.Muted.Render("Δ:        ") + pctStr(s.DeltaPct),
	}
	m.detail.SetContent(strings.Join(lines, "\n"))
}

func (m diffModel) View() string {
	w := m.widthOr(120)
	r := m.result
	pills := []string{
		fmtNs(r.Baseline.TotalDurationNs) + " → " + fmtNs(r.Compare.TotalDurationNs),
		pctStr(r.Summary.DurationDeltaPct),
		fmt.Sprintf("spans %d → %d (%s)", r.Baseline.SpanCount, r.Compare.SpanCount, deltaStr(r.Compare.SpanCount-r.Baseline.SpanCount)),
		fmt.Sprintf("db %s", deltaStr(r.Summary.DBCallDelta)),
	}
	title := fmt.Sprintf("spaniel diff · %s (%s) vs %s (%s)",
		r.Baseline.Label, shortID(r.Baseline.SessionID),
		r.Compare.Label, shortID(r.Compare.SessionID))
	header := tui.HeaderBar(title, pills, w)

	rightW := w / 3
	if rightW < 30 {
		rightW = 30
	}
	left := m.table.View()
	right := tui.Panel.Width(rightW).Height(max(5, m.height-4)).Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Up, tui.Keys.Down, tui.Keys.Quit,
	}, w)
	return tui.JoinVertical(header, body, footer)
}

func (m diffModel) widthOr(d int) int {
	if m.width > 0 {
		return m.width
	}
	return d
}
