package tui

import (
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// foreground returns the foreground color set on a style. The package styles
// only ever set a foreground (an AdaptiveColor), so this is enough to tell two
// of them apart.
func foreground(s lipgloss.Style) lipgloss.TerminalColor {
	return s.GetForeground()
}

func TestSeverityStyle(t *testing.T) {
	tests := []struct {
		name string
		sev  int
		want lipgloss.TerminalColor
	}{
		{"fatal high", 24, colorDanger},
		{"error lower bound", 17, colorDanger},
		{"warn upper bound", 16, colorWarn},
		{"warn lower bound", 13, colorWarn},
		{"info upper bound", 12, colorInk},
		{"info lower bound", 9, colorInk},
		{"debug", 8, colorInk3},
		{"trace", 1, colorInk3},
		{"unknown zero", 0, colorInk3},
		{"unknown negative", -5, colorInk3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foreground(SeverityStyle(tt.sev)); got != tt.want {
				t.Errorf("SeverityStyle(%d) fg = %v, want %v", tt.sev, got, tt.want)
			}
		})
	}
}

func TestStatusStyle(t *testing.T) {
	tests := []struct {
		name string
		code int
		want lipgloss.TerminalColor
	}{
		{"ok zero", 0, colorOk},
		{"ok unset/one", 1, colorOk},
		{"error exactly two", 2, colorDanger},
		{"error above two", 5, colorDanger},
		{"negative treated as ok", -1, colorOk},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foreground(StatusStyle(tt.code)); got != tt.want {
				t.Errorf("StatusStyle(%d) fg = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// TestKeysWellFormed verifies every shared binding has at least one key and a
// non-empty help label, so the footer help text can never render a blank chip.
func TestKeysWellFormed(t *testing.T) {
	bindings := map[string]key.Binding{
		"Quit":     Keys.Quit,
		"Help":     Keys.Help,
		"Up":       Keys.Up,
		"Down":     Keys.Down,
		"Left":     Keys.Left,
		"Right":    Keys.Right,
		"PageUp":   Keys.PageUp,
		"PageDown": Keys.PageDown,
		"Home":     Keys.Home,
		"End":      Keys.End,
		"Enter":    Keys.Enter,
		"Filter":   Keys.Filter,
		"Pause":    Keys.Pause,
		"Follow":   Keys.Follow,
		"Copy":     Keys.Copy,
		"Clear":    Keys.Clear,
		"Tab":      Keys.Tab,
		"ShiftTab": Keys.ShiftTab,
		"Rerun":    Keys.Rerun,
	}
	for name, b := range bindings {
		if len(b.Keys()) == 0 {
			t.Errorf("binding %q has no keys", name)
		}
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("binding %q has empty help: key=%q desc=%q", name, h.Key, h.Desc)
		}
	}
}

// TestKeysMatchExpectedTriggers spot-checks a few bindings to guard against
// accidental key reassignment that would silently break muscle memory.
func TestKeysMatchExpectedTriggers(t *testing.T) {
	cases := []struct {
		b    key.Binding
		want string
	}{
		{Keys.Quit, "q"},
		{Keys.Quit, "ctrl+c"},
		{Keys.Up, "up"},
		{Keys.Up, "k"},
		{Keys.PageDown, " "},
		{Keys.Filter, "/"},
	}
	for _, c := range cases {
		if !keyMatches(c.b, c.want) {
			t.Errorf("expected binding to include trigger %q, keys=%v", c.want, c.b.Keys())
		}
	}
}

func keyMatches(b key.Binding, k string) bool {
	return slices.Contains(b.Keys(), k)
}
