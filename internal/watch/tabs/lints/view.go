package lints

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/client"
)

var muted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)

func severity(s string) string {
	color := "245"
	switch strings.ToLower(s) {
	case "error", "critical":
		color = "203"
	case "warning", "warn":
		color = "214"
	case "info":
		color = "81"
	}
	if s == "" {
		s = "unknown"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(clean(s))
}
func unknown(s string) string {
	if s == "" {
		return "unavailable"
	}
	return clean(s)
}
func (m *model) status() string {
	if m.wrongSession {
		return "Previous-session context"
	}
	parts := []string{}
	if !m.connection.Connected {
		parts = append(parts, "STALE: disconnected; reconnect repairs missed changes")
	}
	names := []string{"lint", "issues"}
	if m.detail == nil {
		names = append(names, "services")
	} else if m.detail.trace != "" || m.detail.span != "" {
		names = append(names, "target")
	}
	for _, name := range names {
		s := m.states[name]
		switch {
		case s.err != nil:
			parts = append(parts, name+" error (retrying; previous data stale): "+clean(s.err.Error()))
		case !s.loaded:
			parts = append(parts, "Loading "+name+"…")
		case s.pending:
			parts = append(parts, name+" refreshing…")
		}
	}
	return strings.Join(parts, " | ")
}
func (m *model) View() string {
	if m.detail != nil {
		return m.detailView()
	}
	semantic, detector := 0, 0
	for _, row := range m.rows {
		if row.source == "detector" {
			detector++
		} else {
			semantic++
		}
	}
	lines := []string{accent.Render(fmt.Sprintf("Findings  %d semantic · %d detector · %d matching", semantic, detector, len(m.visible)))}
	filter := clean(m.filter)
	if filter == "" {
		filter = "all"
	}
	if m.editing {
		lines = append(lines, m.input.View())
	} else {
		lines = append(lines, "Filter: "+filter)
	}
	lines = append(lines, muted.Render("Filter terms: severity:warning rule:http service:api text"))
	if status := m.status(); status != "" {
		lines = append(lines, status)
	} else {
		lines = append(lines, muted.Render("Live · findings are up to date"))
	}
	if len(m.visible) == 0 {
		text := "No findings match this filter."
		if len(m.rows) == 0 {
			text = "No stored findings in this session."
			if !m.states["lint"].loaded || !m.states["issues"].loaded {
				text = "Waiting for finding snapshots; loading or errors shown above."
			}
		}
		lines = append(lines, text)
	} else {
		size := m.pageSize()
		start := (m.cursor / size) * size
		end := min(len(m.visible), start+size)
		for i := start; i < end; i++ {
			row := m.visible[i]
			prefix := "  "
			if i == m.cursor {
				prefix = accent.Render("› ")
			}
			lines = append(lines, prefix+severity(row.severity)+" · "+clean(row.rule)+" · "+row.source+" · "+unknown(row.service)+fmt.Sprintf(" · ×%g", row.count))
			message := row.message
			if row.fingerprint != "" {
				message += " — " + row.fingerprint
			}
			lines = append(lines, "  "+clean(message)+"  [trace "+unknown(row.trace)+" / span "+unknown(row.span)+"]")
		}
		lines = append(lines, muted.Render(fmt.Sprintf("Page %d/%d", start/size+1, (len(m.visible)+size-1)/size)))
	}
	lines = append(lines, muted.Render("Latest ≤2000 findings; service lookup: latest ≤2000 spans; unavailable services do not match filters."))
	return m.fit(lines)
}
func (m *model) detailBody() []string {
	row := m.detail
	lines := []string{accent.Render("Finding · " + clean(row.rule)), m.status()}
	if m.wrongSession {
		lines = append(lines, "Session changed. This finding belongs to the previous session; Back returns to current findings.")
	} else if !m.found {
		lines = append(lines, "Finding removed or expired. Last known context is retained below; navigation is disabled.")
	}
	lines = append(lines, "Source: "+row.source+"   Severity: "+severity(row.severity), "Service: "+unknown(row.service), "Rule / kind: "+clean(row.rule), "Message: "+clean(row.message), fmt.Sprintf("Count: %g", row.count))
	if row.source == "detector" {
		lines = append(lines, "Fingerprint: "+unknown(row.fingerprint), "Wasted time: "+time.Duration(row.wasted).String(), "Parent span: "+unknown(row.parent))
	}
	lines = append(lines, "Session: "+unknown(row.session), "Trace: "+unknown(row.trace), "Span: "+unknown(row.span), "Created: "+created(row.created, m.env.Now()))
	navigation := "No trace/span correlation was recorded."
	if row.trace == "" && row.span != "" {
		navigation = "Resolving correlated span…"
		if m.states["target"].err != nil {
			navigation = "Correlated span unavailable; lookup retrying."
		}
	}
	if row.trace != "" {
		s := m.states["target"]
		var httpErr *client.HTTPError
		switch {
		case errors.As(s.err, &httpErr) && httpErr.Status == 404:
			navigation = "Correlated trace is unavailable (deleted or expired)."
		case s.err != nil:
			navigation = "Correlation lookup failed; retrying. Last known finding remains inspectable."
		case !s.loaded || s.pending:
			navigation = "Checking correlated trace/span…"
		default:
			traceExists, spanExists := false, false
			for _, w := range m.target {
				if w.TraceId == row.trace && w.SessionId == row.session {
					traceExists = true
					if w.SpanId == row.span {
						spanExists = true
					}
				}
			}
			navigation = "Correlated trace is unavailable (deleted or expired)."
			if traceExists {
				navigation = "Correlated trace is available."
				if spanExists && row.span != "" {
					navigation = "Correlated trace and span are available."
				} else if row.span != "" {
					navigation = "Correlated span unavailable (deleted or expired); trace is available."
				}
			}
		}
	}
	source := "lint"
	if row.source == "detector" {
		source = "issues"
	}
	state := m.states[source]
	if !m.found || m.wrongSession || !m.connection.Connected || !state.loaded || state.err != nil || state.pending {
		navigation += " · navigation disabled"
	}
	// Wrap before vertical scrolling so complete messages and IDs remain accessible.
	body := []string{}
	lines = append(lines, navigation)
	for _, line := range lines {
		body = append(body, strings.Split(ansi.Wrap(line, max(1, m.width), ""), "\n")...)
	}
	return body
}

func (m *model) clampScroll() {
	if m.detail != nil {
		m.scroll = max(0, min(m.scroll, len(m.detailBody())-m.detailHeight()))
	}
}

func (m *model) detailHeight() int { return max(1, m.height-1) }

func (m *model) detailView() string {
	body := m.detailBody()
	height := m.detailHeight()
	start := min(m.scroll, max(0, len(body)-height))
	end := min(len(body), start+height)
	visible := append([]string(nil), body[start:end]...)
	visible = append(visible, muted.Render(fmt.Sprintf("Lines %d–%d/%d", start+1, end, len(body))))
	return m.fit(visible)
}
func (m *model) fit(lines []string) string {
	lines = lines[:min(len(lines), m.height)]
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "…")
	}
	return strings.Join(lines, "\n")
}
