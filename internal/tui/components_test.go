package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

func TestHeaderBar_FillsWidth(t *testing.T) {
	const width = 60
	out := HeaderBar("spaniel watch", nil, width)
	// The bar should render exactly the requested width on its content line.
	if w := lipgloss.Width(firstLine(out)); w != width {
		t.Errorf("HeaderBar width = %d, want %d", w, width)
	}
	if !strings.Contains(out, "spaniel watch") {
		t.Errorf("HeaderBar missing title, got %q", out)
	}
}

func TestHeaderBar_SkipsEmptyPills(t *testing.T) {
	withEmpty := HeaderBar("t", []string{"", "live", ""}, 40)
	withOne := HeaderBar("t", []string{"live"}, 40)
	if stripANSI(withEmpty) != stripANSI(withOne) {
		t.Errorf("empty pills should be skipped:\n empty=%q\n one=%q", withEmpty, withOne)
	}
	if !strings.Contains(withEmpty, "live") {
		t.Errorf("expected 'live' pill rendered, got %q", withEmpty)
	}
}

func TestHeaderBar_NarrowWidthDoesNotPanic(t *testing.T) {
	// Width smaller than the content forces the pad-floor branch (pad < 1).
	// The title wraps, but the call must not panic and must still emit the
	// title's characters somewhere in the output.
	out := HeaderBar("a very long title that exceeds width", []string{"pill"}, 5)
	if !strings.Contains(stripANSI(out), "t") || out == "" {
		t.Errorf("expected non-empty rendered header, got %q", out)
	}
}

func TestFooterHelp_RendersBoundKeys(t *testing.T) {
	out := FooterHelp([]key.Binding{Keys.Quit, Keys.Help}, 80)
	for _, want := range []string{"q", "quit", "?", "help"} {
		if !strings.Contains(out, want) {
			t.Errorf("FooterHelp missing %q, got %q", want, out)
		}
	}
}

func TestFooterHelp_SkipsBindingsWithoutHelp(t *testing.T) {
	// A binding with no WithHelp has an empty Help().Key and must be dropped.
	blank := key.NewBinding(key.WithKeys("x"))
	out := FooterHelp([]key.Binding{blank, Keys.Quit}, 80)
	if !strings.Contains(out, "quit") {
		t.Errorf("expected real binding rendered, got %q", out)
	}
	// Nothing meaningful from the blank binding should appear.
	if strings.Contains(stripANSI(out), "x ") {
		t.Errorf("blank binding should be skipped, got %q", out)
	}
}

func TestFooterHelp_EmptyBindings(t *testing.T) {
	out := FooterHelp(nil, 30)
	// Still renders the footer chrome at the requested width without panicking.
	if w := lipgloss.Width(firstLine(out)); w != 30 {
		t.Errorf("empty FooterHelp width = %d, want 30", w)
	}
}

func TestJoinVertical(t *testing.T) {
	out := JoinVertical("alpha", "beta", "gamma")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 stacked lines, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "alpha") ||
		!strings.HasPrefix(lines[1], "beta") ||
		!strings.HasPrefix(lines[2], "gamma") {
		t.Errorf("lines not left-aligned/ordered: %q", lines)
	}
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

// stripANSI removes SGR escape sequences so structural comparisons ignore
// color codes (which depend on terminal capability detection).
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if c == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
