package shell

import (
	"fmt"
	"strings"
	"unicode"

	keys "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

type contextualHelp interface{ ShortHelp() []keys.Binding }

func (m *Model) helpContext() string {
	s := m.owner()
	if provider, ok := m.focused().(contextualHelp); ok {
		for _, b := range provider.ShortHelp() {
			if b.Enabled() {
				s += "\x00" + b.Help().Key + "\x00" + b.Help().Desc
			}
		}
	}
	return s
}

func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func shortcut(k, description string) string {
	return tui.Accent.Bold(true).Underline(true).Render(k) + tui.Muted.Render(" "+description)
}

// Every binding remains discoverable even in small windows. F1 cycles help
// pages without capturing printable input or changing the focused model.
func (m *Model) footer() []string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}
	var items []string
	if provider, ok := m.focused().(contextualHelp); ok {
		for _, binding := range provider.ShortHelp() {
			if binding.Enabled() {
				h := binding.Help()
				items = append(items, shortcut(singleLine(h.Key), singleLine(h.Desc)))
			}
		}
	}
	var globals []string
	capturing := false
	if capture, ok := m.focused().(protocol.KeyCapture); ok {
		capturing = capture.CapturesKeys()
	}
	if !capturing {
		globals = append(globals, shortcut("Tab/Shift-Tab", "tabs"))
		if len(m.stack) > 0 {
			globals = append(globals, shortcut("Esc/Backspace", "back"))
		}
		globals = append(globals, shortcut("q", "quit"))
	}
	globals = append(globals, shortcut("Ctrl+C", "quit"))
	globalRows := wrapHelp(globals, width)
	capacity := max(1, min(3, height-3-len(globalRows)))
	rows := wrapHelp(items, width)
	pages := max(1, (len(rows)+capacity-1)/capacity)
	page := m.helpPage % pages
	start := page * capacity
	out := append([]string(nil), rows[start:min(len(rows), start+capacity)]...)
	out = append(out, globalRows...)
	state := tui.Warn.Bold(true).Render("◌ connecting")
	if m.status.Connected {
		state = tui.Ok.Bold(true).Render("● live")
	} else if m.status.Err != nil {
		state = tui.Warn.Bold(true).Render("● reconnecting") + tui.Muted.Render(": "+singleLine(m.status.Err.Error()))
	}
	if pages > 1 {
		state = shortcut("F1", fmt.Sprintf("more keys %d/%d", page+1, pages)) + tui.Faint.Render(" │ ") + state
	}
	out = append(out, ansi.Truncate(state, width, "…"))
	// Tiny terminals cannot hold header, content and all help simultaneously.
	// Page the same complete binding list there; normal layouts keep globals
	// visible on every contextual help page.
	if len(out) > max(1, height-2) {
		all := append(rows, globalRows...)
		count := max(1, height-3)
		n := max(1, (len(all)+count-1)/count)
		i := (m.helpPage % n) * count
		out = append([]string(nil), all[i:min(len(all), i+count)]...)
		out = append(out, ansi.Truncate(shortcut("F1", fmt.Sprintf("keys %d/%d", m.helpPage%n+1, n)), width, "…"))
	}
	return out
}

func wrapHelp(items []string, width int) []string {
	var rows []string
	line := ""
	for _, item := range items {
		// Wrap individual long bindings too, preserving their complete action.
		if line != "" && ansi.StringWidth(line)+3+ansi.StringWidth(item) > width {
			rows = append(rows, line)
			line = ""
		}
		parts := strings.Split(ansi.Hardwrap(item, width, true), "\n")
		for _, part := range parts[:len(parts)-1] {
			rows = append(rows, part)
		}
		if line != "" {
			line += tui.Faint.Render(" · ")
		}
		line += parts[len(parts)-1]
	}
	if line != "" {
		rows = append(rows, line)
	}
	return rows
}

func (m *Model) contentHeight() int {
	height := m.height
	if height <= 0 {
		height = 30
	}
	return max(0, height-1-len(m.footer()))
}
