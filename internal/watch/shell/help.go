package shell

import (
	"fmt"
	"sort"
	"strings"

	keys "github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type contextualHelp interface{ ShortHelp() []keys.Binding }

func (m *Model) capturesKeys() bool {
	capture, ok := m.focused().(protocol.KeyCapture)
	return ok && capture.CapturesKeys()
}
func helpBinding(press, label, action string) keys.Binding {
	return keys.NewBinding(keys.WithKeys(press), keys.WithHelp(label, action))
}
func (m *Model) bindings() []keys.Binding {
	var out []keys.Binding
	if provider, ok := m.focused().(contextualHelp); ok {
		for _, b := range provider.ShortHelp() {
			if b.Enabled() {
				out = append(out, b)
			}
		}
	}
	if !m.capturesKeys() {
		out = append(out, helpBinding("tab", "Tab/Shift-Tab", "tabs"))
		if len(m.stack) > 0 {
			out = append(out, helpBinding("esc", "Esc/Backspace", "back"))
		}
		out = append(out, helpBinding("q", "q", "quit"))
	}
	return append(out, helpBinding("ctrl+c", "Ctrl+C", "quit"))
}
func (m *Model) helpContext() string {
	s := m.owner()
	for _, b := range m.bindings() {
		s += "\x00" + b.Help().Key + "\x00" + b.Help().Desc
	}
	return s
}
func singleLine(s string) string { return terminal.SingleLine(s) }

func shortcut(k, description string) string {
	return tui.Accent.Bold(true).Underline(true).Render(k) + tui.Muted.Render(" "+description)
}
func (m *Model) dimensions() (int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	return width, height
}
func (m *Model) contentHeight() int { _, height := m.dimensions(); return max(0, height-2) }

// The shortcut area is exactly one physical row. Every omitted binding remains
// in the focused popup; printable '?' stays literal inside captured text.
func (m *Model) footer() []string {
	width, _ := m.dimensions()
	if m.helpOpen {
		return []string{ansi.Truncate(shortcut("Esc/F2", "close")+" · "+shortcut("↑/↓", "scroll")+" · "+shortcut("PgUp/PgDn", "page"), width, "…")}
	}
	helpKey := "?"
	if m.capturesKeys() {
		helpKey = "F2"
	}
	hint := shortcut(helpKey, "help")
	bindings := m.bindings()
	var all []string
	for _, b := range bindings {
		h := b.Help()
		all = append(all, shortcut(singleLine(h.Key), singleLine(h.Desc)))
	}
	separator := tui.Faint.Render(" · ")
	full := strings.Join(append(all, hint), separator)
	if ansi.StringWidth(full) <= width {
		return []string{full}
	}
	var global []string
	if m.capturesKeys() {
		global = append(global, shortcut("Ctrl+C", "quit"))
	} else {
		global = append(global, shortcut("Tab/⇧Tab", "tabs"))
		if len(m.stack) > 0 {
			global = append(global, shortcut("Esc", "back"))
		}
		global = append(global, shortcut("q", "quit"))
	}
	// Keep the help trigger visible even when a terminal cannot hold every
	// global action. The complete applicable globals are always in the popup.
	for len(global) > 0 && ansi.StringWidth(strings.Join(append(global, hint), separator)) > max(ansi.StringWidth(hint), width/2) {
		global = global[1:]
	}
	suffix := strings.Join(append(global, hint), separator)
	budget := width - ansi.StringWidth(suffix) - 3
	var features []keys.Binding
	if provider, ok := m.focused().(contextualHelp); ok {
		features = provider.ShortHelp()
	}
	// Confirmation and telemetry navigation precede less frequent scroll/page
	// aliases; overflow keeps every action in the popup.
	ordered := append([]keys.Binding(nil), features...)
	rank := func(b keys.Binding) int {
		for _, press := range b.Keys() {
			if press == "enter" {
				return 0
			}
		}
		action := b.Help().Desc
		if strings.Contains(action, "containing trace") || strings.Contains(action, "correlated span") || strings.Contains(action, "open trace") || strings.Contains(action, "open span") || strings.Contains(action, "telemetry links") {
			return 1
		}
		for _, press := range b.Keys() {
			if press == "/" {
				return 2
			}
			if press == "up" || press == "down" {
				return 3
			}
		}
		for _, press := range b.Keys() {
			if press == "p" || press == "w" || press == "o" {
				return 4
			}
		}
		return 5
	}
	sort.SliceStable(ordered, func(i, j int) bool { return rank(ordered[i]) < rank(ordered[j]) })
	var selected []string
	for _, b := range ordered {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		item := shortcut(singleLine(h.Key), singleLine(h.Desc))
		candidate := strings.Join(append(selected, item), separator)
		if ansi.StringWidth(candidate) <= budget {
			selected = append(selected, item)
		}
	}
	prefix := strings.Join(selected, separator)
	if prefix == "" && budget > 0 && len(ordered) > 0 {
		h := ordered[0].Help()
		prefix = ansi.Truncate(shortcut(singleLine(h.Key), singleLine(h.Desc)), budget, "…")
	}
	if prefix != "" {
		suffix = prefix + separator + suffix
	}
	return []string{ansi.Truncate(suffix, width, "…")}
}
func (m *Model) connectionLabel() string {
	width, _ := m.dimensions()
	state := tui.Warn.Render("◌ connecting")
	if m.status.Connected {
		state = tui.Ok.Bold(true).Render("● live")
	} else if m.status.Err != nil {
		state = tui.Warn.Bold(true).Render("● reconnecting") + tui.Muted.Render(": "+singleLine(m.status.Err.Error()))
	}
	return ansi.Truncate(state, max(8, width/3), "…")
}

func (m *Model) helpRows(width int) []string {
	var rows []string
	for _, b := range m.bindings() {
		h := b.Help()
		text := shortcut(singleLine(h.Key), singleLine(h.Desc))
		rows = append(rows, strings.Split(ansi.Hardwrap(text, max(1, width), true), "\n")...)
	}
	return rows
}
func (m *Model) helpSize() (int, int, int) {
	width, _ := m.dimensions()
	popupWidth := max(1, min(84, width-4))
	inner := max(1, popupWidth-4)
	return popupWidth, inner, max(1, m.contentHeight()-6)
}
func (m *Model) popupKey(msg tea.KeyMsg) tea.Cmd {
	_, inner, capacity := m.helpSize()
	limit := max(0, len(m.helpRows(inner))-capacity)
	switch msg.String() {
	case "esc", "f2", "?":
		m.helpOpen = false
		m.helpOffset = 0
	case "up", "k":
		m.helpOffset = max(0, m.helpOffset-1)
	case "down", "j":
		m.helpOffset = min(limit, m.helpOffset+1)
	case "pgup":
		m.helpOffset = max(0, m.helpOffset-capacity)
	case "pgdown", " ":
		m.helpOffset = min(limit, m.helpOffset+capacity)
	case "home":
		m.helpOffset = 0
	case "end":
		m.helpOffset = limit
	case "q":
		if !m.capturesKeys() {
			return tea.Quit
		}
	}
	return nil
}
func (m *Model) overlayHelp(body []string) []string {
	if !m.helpOpen || len(body) == 0 {
		return body
	}
	width, _ := m.dimensions()
	popupWidth, inner, capacity := m.helpSize()
	rows := m.helpRows(inner)
	offset := min(m.helpOffset, max(0, len(rows)-capacity))
	end := min(len(rows), offset+capacity)
	text := tui.Title.Render(ansi.Truncate("Keyboard help · "+m.tabs[m.active].Title, inner, "…")) + "\n" + strings.Join(rows[offset:end], "\n") + "\n" + tui.Muted.Render(fmt.Sprintf("%d–%d / %d", offset+1, end, len(rows)))
	if width < 8 || len(body) < 5 {
		parts := strings.Split(text, "\n")
		for i := range body {
			if i < len(parts) {
				body[i] = ansi.Truncate(parts[i], width, "…")
			}
		}
		return body
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#79D5CA")).Background(lipgloss.Color("#161E29")).Padding(0, 1).Width(inner + 2).Render(text)
	lines := strings.Split(box, "\n")
	if len(lines) > len(body) {
		lines = lines[:len(body)]
	}
	left, top := max(0, (width-popupWidth)/2), max(0, (len(body)-len(lines))/2)
	for i, line := range lines {
		original := body[top+i]
		if ansi.StringWidth(original) < width {
			original += strings.Repeat(" ", width-ansi.StringWidth(original))
		}
		body[top+i] = ansi.Cut(original, 0, left) + line + ansi.Cut(original, left+ansi.StringWidth(line), width)
	}
	return body
}
