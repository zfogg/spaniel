package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// HeaderBar renders the top chrome for a TUI screen.
//
//	title    — bolded screen name (e.g. "spaniel watch")
//	pills    — small badges shown to the right of the title (active filters,
//	           connection status). Empty strings are skipped.
//	width    — total terminal width; the bar fills it.
func HeaderBar(title string, pills []string, width int) string {
	left := Title.Render(title)
	var right string
	if len(pills) > 0 {
		var rendered []string
		for _, p := range pills {
			if p == "" {
				continue
			}
			rendered = append(rendered, Badge.Render(p))
		}
		right = strings.Join(rendered, " ")
	}
	pad := width - lipgloss.Width(left) - lipgloss.Width(right) - 2 // 2 = Header padding
	if pad < 1 {
		pad = 1
	}
	return Header.Width(width).Render(left + strings.Repeat(" ", pad) + right)
}

// FooterHelp renders a one-line footer showing a subset of bound keys.
// Pass only the bindings that are actually active right now so the user's
// eyes don't have to filter noise.
func FooterHelp(bindings []key.Binding, width int) string {
	var parts []string
	for _, b := range bindings {
		h := b.Help()
		if h.Key == "" {
			continue
		}
		parts = append(parts, KeyHint.Render(h.Key)+" "+Muted.Render(h.Desc))
	}
	body := strings.Join(parts, "  ")
	return Footer.Width(width).Render(body)
}

// JoinVertical is a thin shim over lipgloss.JoinVertical that picks Left
// alignment — the right default for top-down stacked screens.
func JoinVertical(parts ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
