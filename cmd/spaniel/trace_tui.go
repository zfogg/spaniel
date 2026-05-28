package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zfogg/spaniel/internal/tui"
)

// traceTUI launches the interactive waterfall viewer for a single trace.
// Layout: header (trace id + summary) / waterfall pane (left) + detail
// pane (right) / footer.
func traceTUI(spans []*traceSpan) error {
	m := newTraceModel(spans)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type traceModel struct {
	spans     []*traceSpan // sorted by start_ns
	depths    map[string]int
	traceID   string
	traceDur  int64
	traceStart int64

	cursor int
	width  int
	height int

	detail viewport.Model
}

func newTraceModel(raw []*traceSpan) traceModel {
	sorted := sortSpansByStart(raw)
	start, end := traceWindow(raw)
	dur := end - start
	if dur <= 0 {
		dur = 1
	}
	return traceModel{
		spans:      sorted,
		depths:     computeDepths(raw),
		traceID:    sorted[0].TraceID,
		traceDur:   dur,
		traceStart: start,
		detail:     viewport.New(40, 20),
	}
}

func (m traceModel) Init() tea.Cmd { return nil }

func (m traceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Right pane is ~1/3 of the width, min 30 cols.
		rightW := m.width / 3
		if rightW < 30 {
			rightW = 30
		}
		m.detail.Width = rightW
		m.detail.Height = max(5, m.height-4)
		m.refreshDetail()
		return m, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, tui.Keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.refreshDetail()
			}
		case key.Matches(msg, tui.Keys.Down):
			if m.cursor < len(m.spans)-1 {
				m.cursor++
				m.refreshDetail()
			}
		case key.Matches(msg, tui.Keys.Home):
			m.cursor = 0
			m.refreshDetail()
		case key.Matches(msg, tui.Keys.End):
			m.cursor = len(m.spans) - 1
			m.refreshDetail()
		}
	}
	return m, nil
}

func (m *traceModel) refreshDetail() {
	s := m.spans[m.cursor]
	lines := []string{
		tui.Title.Render(s.ServiceName + " · " + s.Name),
		"",
		tui.Muted.Render("span id:    ") + s.SpanID,
		tui.Muted.Render("parent:     ") + ifEmpty(s.ParentSpanID, "(root)"),
		tui.Muted.Render("duration:   ") + humanNs(s.DurationNs),
		tui.Muted.Render("start (ns): ") + fmt.Sprint(s.StartNs),
		tui.Muted.Render("end (ns):   ") + fmt.Sprint(s.EndNs),
		tui.Muted.Render("depth:      ") + fmt.Sprint(m.depths[s.SpanID]),
		tui.Muted.Render("status:     ") + statusLabel(s.StatusCode),
	}
	if s.Tag != "" {
		lines = append(lines, tui.Muted.Render("tag:        ")+tui.Warn.Render(tagLabel(s.Tag)))
	}
	m.detail.SetContent(strings.Join(lines, "\n"))
}

func (m traceModel) View() string {
	w := m.widthOr(120)
	rightW := w / 3
	if rightW < 30 {
		rightW = 30
	}
	leftW := w - rightW - 2 // 2 for gutter
	if leftW < 20 {
		leftW = 20
	}

	pills := []string{
		fmt.Sprintf("%d spans", len(m.spans)),
		humanNs(m.traceDur),
		"trace " + m.traceID[:8],
	}
	header := tui.HeaderBar("spaniel trace", pills, w)

	waterfall := m.renderWaterfall(leftW)
	right := tui.Panel.Width(rightW).Height(max(5, m.height-4)).Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, waterfall, " ", right)

	footer := tui.FooterHelp([]key.Binding{
		tui.Keys.Up, tui.Keys.Down, tui.Keys.Home, tui.Keys.End, tui.Keys.Quit,
	}, w)

	return tui.JoinVertical(header, body, footer)
}

// renderWaterfall lays out one row per span. The left half is the label
// (indented by depth), the right is the duration bar scaled to barWidth.
func (m traceModel) renderWaterfall(totalWidth int) string {
	const labelW = 44
	barW := totalWidth - labelW - 12 // 8 dur + 4 padding
	if barW < 10 {
		barW = 10
	}
	rows := make([]string, 0, len(m.spans))
	for i, s := range m.spans {
		offset := int(float64(s.StartNs-m.traceStart) / float64(m.traceDur) * float64(barW))
		barLen := int(float64(s.DurationNs) / float64(m.traceDur) * float64(barW))
		if barLen < 1 {
			barLen = 1
		}
		if offset+barLen > barW {
			barLen = barW - offset
		}
		bar := strings.Repeat(" ", offset) + strings.Repeat("█", barLen) + strings.Repeat(" ", barW-offset-barLen)

		indent := strings.Repeat("  ", m.depths[s.SpanID])
		label := indent + s.ServiceName + " · " + s.Name
		label = truncate(label, labelW)
		label = padRight(label, labelW)

		dur := padLeft(humanNs(s.DurationNs), 8)

		style := tui.StatusStyle(s.StatusCode)
		barStyled := style.Render(bar)
		line := fmt.Sprintf("%s %s |%s|", label, dur, barStyled)
		if s.Tag != "" {
			line += " " + tui.Warn.Render(tagLabel(s.Tag))
		}
		if i == m.cursor {
			line = tui.SelectedRow.Render(line)
		}
		rows = append(rows, line)
	}
	body := strings.Join(rows, "\n")
	// Wrap in a viewport-shaped box for height limiting. Simple slice for now.
	maxLines := max(5, m.height-4)
	if len(rows) > maxLines {
		// Window around cursor.
		start := m.cursor - maxLines/2
		if start < 0 {
			start = 0
		}
		end := start + maxLines
		if end > len(rows) {
			end = len(rows)
			start = end - maxLines
		}
		body = strings.Join(rows[start:end], "\n")
	}
	return body
}

func (m traceModel) widthOr(d int) int {
	if m.width > 0 {
		return m.width
	}
	return d
}

func statusLabel(code int) string {
	switch {
	case code >= 2:
		return tui.Danger.Render("ERROR")
	case code == 1:
		return tui.Ok.Render("OK")
	default:
		return tui.Muted.Render("UNSET")
	}
}

func ifEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func padRight(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}

func padLeft(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-lipgloss.Width(s)) + s
}
