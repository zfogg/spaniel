// Package terminal makes untrusted telemetry safe to display in a terminal.
package terminal

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// SingleLine removes escape sequences and replaces line/control characters.
func SingleLine(value string) string { return sanitize(value, false) }

// Multiline preserves explicit newlines for full-value inspectors.
func Multiline(value string) string { return sanitize(value, true) }

func sanitize(value string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' && multiline {
			return r
		}
		if unicode.In(r, unicode.Cf) {
			return -1 // Includes bidirectional display overrides.
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			if multiline {
				return -1
			}
			return ' '
		}
		if r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, ansi.Strip(value))
}

func Wrap(value string, width int) []string {
	return strings.Split(ansi.Hardwrap(Multiline(value), max(1, width), true), "\n")
}
