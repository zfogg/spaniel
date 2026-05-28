// Package tui is the shared foundation for Spaniel's terminal UIs (Bubble Tea
// + Lipgloss). All TUI-mode subcommands pull their colors, spacing, and small
// reusable widgets from this package so the look stays consistent.
//
// The palette mirrors the frontend "Drift" theme (sky/sand/slate) — see
// frontend/src/index.css and the project_design_system memory.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette holds the raw color values used across all TUI views. Light/dark
// adaptive — Lipgloss picks the right side based on the user's terminal
// background detection. Keep the keys in lockstep with the CSS vars they
// mirror so the TUI and web UI stay visually unified.
var (
	colorBg       = lipgloss.AdaptiveColor{Light: "#e4eaf0", Dark: "#0c1622"}
	colorSurface  = lipgloss.AdaptiveColor{Light: "#f5f9fc", Dark: "#111f2e"}
	colorSurface2 = lipgloss.AdaptiveColor{Light: "#dfe7ee", Dark: "#162638"}
	colorInk      = lipgloss.AdaptiveColor{Light: "#1f2a37", Dark: "#dbe7f2"}
	colorInk2     = lipgloss.AdaptiveColor{Light: "#4b5563", Dark: "#94a3b8"}
	colorInk3     = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#64748b"}
	colorLine     = lipgloss.AdaptiveColor{Light: "#cdd5df", Dark: "#1f3147"}
	colorAccent   = lipgloss.AdaptiveColor{Light: "#7aa3c4", Dark: "#7aa3c4"}
	colorOk       = lipgloss.AdaptiveColor{Light: "#88b29a", Dark: "#88b29a"}
	colorWarn     = lipgloss.AdaptiveColor{Light: "#d6b46a", Dark: "#d6b46a"}
	colorDanger   = lipgloss.AdaptiveColor{Light: "#c8786e", Dark: "#d69882"}
)

// Style registry. These are package-level vars so callers can do
// `tui.Danger.Render("x")` without setup. Lipgloss styles are
// copy-on-write — embedding one and chaining `.Bold(true)` etc. is cheap.
var (
	Ink     = lipgloss.NewStyle().Foreground(colorInk)
	Muted   = lipgloss.NewStyle().Foreground(colorInk2)
	Faint   = lipgloss.NewStyle().Foreground(colorInk3)
	Accent  = lipgloss.NewStyle().Foreground(colorAccent)
	Ok      = lipgloss.NewStyle().Foreground(colorOk)
	Warn    = lipgloss.NewStyle().Foreground(colorWarn)
	Danger  = lipgloss.NewStyle().Foreground(colorDanger)

	// Bold accent for things like header titles and active-tab labels.
	Title   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	// Header bar across the top of every screen — bottom border in --line.
	Header  = lipgloss.NewStyle().
		Foreground(colorInk).
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(colorLine).
		Padding(0, 1)
	// Footer with help/keys — top border, muted ink.
	Footer  = lipgloss.NewStyle().
		Foreground(colorInk2).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(colorLine).
		Padding(0, 1)
	// KeyHint matches the inline `<kbd>` chips on the web side.
	KeyHint = lipgloss.NewStyle().
		Foreground(colorInk).
		Background(colorSurface2).
		Padding(0, 1)
	// Badge — small inline tag (severity, status, kind).
	Badge   = lipgloss.NewStyle().Padding(0, 1).Bold(true)

	// Panel — bordered box for side panes (trace attributes, doctor detail).
	Panel = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorLine).
		Padding(0, 1)

	// SelectedRow — used by tables/lists to highlight focus.
	SelectedRow = lipgloss.NewStyle().
		Foreground(colorInk).
		Background(colorSurface2).
		Bold(true)

	// Toast — error/warning overlays.
	Toast = lipgloss.NewStyle().
		Foreground(colorDanger).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorDanger).
		Padding(0, 1)
)

// SeverityStyle returns the right style for an OTel severity number.
// Mirrors the colorization in the old printf path so logs/spans look
// consistent across all views.
func SeverityStyle(sev int) lipgloss.Style {
	switch {
	case sev >= 17: // ERROR/FATAL
		return Danger
	case sev >= 13: // WARN
		return Warn
	case sev >= 9: // INFO
		return Ink
	default: // DEBUG/TRACE/unknown
		return Faint
	}
}

// StatusStyle picks a style based on an OTel span status code.
// 2 = ERROR, anything else = OK.
func StatusStyle(code int) lipgloss.Style {
	if code >= 2 {
		return Danger
	}
	return Ok
}
