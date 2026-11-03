package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/servicecolor"
)

// Content roles are separate from the existing shell/footer tokens. These add
// no padding or borders: styling cannot change the layout or scrolling geometry.
var (
	contentSky      = lipgloss.AdaptiveColor{Light: "#284a6a", Dark: "#a8c4dc"}
	contentRed      = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	contentAmber    = lipgloss.AdaptiveColor{Light: "#6a4f1e", Dark: "#f0d898"}
	ContentTitle    = lipgloss.NewStyle().Foreground(contentSky).Bold(true)
	ContentHeading  = lipgloss.NewStyle().Foreground(contentSky).Bold(true).Underline(true).UnderlineSpaces(false)
	ContentLabel    = lipgloss.NewStyle().Foreground(colorInk2)
	ContentValue    = lipgloss.NewStyle().Foreground(colorInk)
	ContentStrong   = lipgloss.NewStyle().Foreground(colorInk).Bold(true)
	ContentContext  = lipgloss.NewStyle().Foreground(colorInk2).Italic(true)
	ContentRule     = lipgloss.NewStyle().Foreground(colorInk3)
	ContentMeasure  = lipgloss.NewStyle().Foreground(contentSky).Bold(true)
	ContentChart    = lipgloss.NewStyle().Foreground(colorAccent)
	ContentWarning  = lipgloss.NewStyle().Foreground(contentAmber).Bold(true)
	ContentError    = lipgloss.NewStyle().Foreground(contentRed).Bold(true)
	ContentOK       = lipgloss.NewStyle().Foreground(colorOk)
	ContentSelected = lipgloss.NewStyle().Foreground(colorInk).Background(colorSurface2).Bold(true)
)

// ContentEnabled follows the renderer's detected depth; never force a profile.
// NO_COLOR/plain/dumb suppress attributes too, providing genuinely plain content.
func ContentEnabled() bool {
	return os.Getenv("NO_COLOR") == "" && !strings.EqualFold(os.Getenv("TERM"), "dumb") && lipgloss.ColorProfile() != termenv.Ascii
}

// RenderContent receives already safe, laid-out text from presentation code.
func RenderContent(style lipgloss.Style, text string) string {
	if !ContentEnabled() {
		return ansi.Strip(text)
	}
	return style.Render(text)
}

func ContentRow(text string, selected bool) string {
	if selected && ContentEnabled() && text != "" {
		// Nested semantic styles reset SGR at their end. Restore the selected
		// background after each reset, preserving their foreground/severity.
		sample := ContentSelected.Render(" ")
		if at := strings.IndexByte(sample, ' '); at > 0 {
			start := sample[:at]
			text = strings.NewReplacer("\x1b[0m", "\x1b[0m"+start, "\x1b[m", "\x1b[m"+start).Replace(text)
			return start + text + "\x1b[0m"
		}
	}
	return RenderContent(ContentValue, text)
}

func ContentPair(label, value string) string {
	return RenderContent(ContentLabel, label) + RenderContent(ContentValue, value)
}

func ContentSeverityStyle(severity int) lipgloss.Style {
	switch {
	case severity >= 21:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#7e22ce")).Bold(true)
	case severity >= 17:
		return ContentError
	case severity >= 13:
		return ContentWarning
	case severity >= 9:
		return lipgloss.NewStyle().Foreground(contentSky)
	default:
		return ContentLabel
	}
}

// RenderServiceName hashes the canonical name before display sanitization,
// clipping or fallback. display must already be safe and laid out by the caller.
// Lower-depth terminals keep the readable theme instead of nearest-color hashes.
func RenderServiceName(canonical, display string) string {
	return RenderContent(ServiceNameStyle(canonical), display)
}

// ServiceNameStyle is also used by Charmbracelet's per-cell table styling.
func ServiceNameStyle(canonical string) lipgloss.Style {
	style := ContentStrong
	if ContentEnabled() && lipgloss.ColorProfile() == termenv.TrueColor {
		color := servicecolor.Color(canonical).Text(lipgloss.HasDarkBackground())
		style = style.Foreground(lipgloss.Color(color.Hex()))
	}
	return style
}
