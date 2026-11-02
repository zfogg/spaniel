package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/client"
)

// All data is sanitized before styles are applied. Styles originate here, never
// in dashboard names, SQL, variables or query results received from the server.
var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6EDF3"))
	selectedStyle  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#79D5CA"))
	captionStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8CC8F1"))
	secondaryStyle = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#8996A8"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8996A8"))
	ruleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#3C4C61"))
	valueStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#79D5CA"))
	chartStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#79D5CA"))
	warningStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E9C46A"))
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F08A8A"))
)

type uiLine struct {
	text  string
	style lipgloss.Style
	clip  bool
}

func ui(text string, style lipgloss.Style) uiLine { return uiLine{text: text, style: style} }
func compact(text string, style lipgloss.Style) uiLine {
	return uiLine{text: text, style: style, clip: true}
}
func plain(text string) uiLine { return ui(text, lipgloss.NewStyle()) }
func wrapUI(lines []uiLine, width int) []string {
	var out []string
	for _, line := range lines {
		text := safe(line.text)
		if line.clip {
			text = ansi.Truncate(text, max(1, width), "…")
		}
		for _, part := range strings.Split(ansi.Hardwrap(text, max(1, width), true), "\n") {
			out = append(out, line.style.Render(part))
		}
	}
	return out
}
func shortID(id string) string {
	if id == "" {
		return "default"
	}
	if len(id) > 12 {
		return id[:8] + "…"
	}
	return id
}
func readableError(err error) string {
	var response *client.HTTPError
	if errors.As(err, &response) {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(response.Message), &body) == nil && body.Error != "" {
			return fmt.Sprintf("HTTP %d · %s", response.Status, body.Error)
		}
	}
	return err.Error()
}
func (m *model) notices() []uiLine {
	var lines []uiLine
	if !m.connection.Connected {
		lines = append(lines, compact("STALE · reconnecting · retained data", warningStyle))
	}
	if m.definitionStatus != "" {
		lines = append(lines, compact(m.definitionStatus, warningStyle))
	}
	if m.definitionPending {
		lines = append(lines, compact("STALE · definition changes pending repair", warningStyle))
	}
	if m.err != nil {
		lines = append(lines, compact("Definition error (retained data): "+readableError(m.err), errorStyle))
	}
	return lines
}
func (m *model) compose(top, body []uiLine, selectedEnd int) string {
	header := wrapUI(top, m.width)
	header = header[:min(len(header), max(0, m.height-1))]
	available := max(1, m.height-len(header))
	content := wrapUI(body, m.width)
	if available < 4 {
		for len(content) > 0 && strings.TrimSpace(ansi.Strip(content[0])) == "" {
			content = content[1:]
		}
	}
	offset := 0
	if m.detailID != "" {
		m.scroll = min(m.scroll, max(0, len(content)-available))
		offset = m.scroll
	} else if selectedEnd >= available {
		offset = min(max(0, len(content)-available), selectedEnd)
	}
	end := min(len(content), offset+available)
	rows := append(header, content[offset:end]...)
	for len(rows) < m.height {
		rows = append(rows, "")
	}
	return strings.Join(rows[:min(len(rows), max(1, m.height))], "\n")
}
func (m *model) View() string {
	top := m.notices()
	if !m.loaded {
		return m.compose(append(top, compact("Dashboards", titleStyle)), []uiLine{ui("Loading saved dashboards…", secondaryStyle)}, 0)
	}
	if m.detailID == "" {
		return m.listView(top)
	}
	if m.removed {
		return m.compose(top, []uiLine{ui("Dashboard removed externally.", errorStyle), ui("This saved view is no longer available.", secondaryStyle)}, 0)
	}
	d := m.dashboard()
	if d == nil {
		return m.compose(top, []uiLine{ui("Loading definition…", secondaryStyle)}, 0)
	}
	top = append(top, compact(d.Name, titleStyle))
	window := "Saved window"
	if m.live {
		window = "Live window · 15m"
	}
	top = append(top, compact(fmt.Sprintf("Session %s   ·   %s   ·   Read-only", shortID(m.session), window), mutedStyle))
	if len(d.Variables) > 0 {
		v := d.Variables[m.variable]
		value := v.DefaultValue
		if override, ok := m.overrides[v.Name]; ok {
			value = override
		}
		top = append(top, compact(fmt.Sprintf("Filter %d/%d   $%s = %s", m.variable+1, len(d.Variables), v.Name, value), captionStyle))
	}
	if m.editing {
		return m.inputView(top, d)
	}
	if m.inputError != "" {
		top = append(top, compact(m.inputError, errorStyle))
	}
	if len(d.Panels) == 0 {
		return m.compose(top, []uiLine{ui("No panels in this dashboard.", secondaryStyle)}, 0)
	}
	index := m.panelIndex()
	p := d.Panels[index]
	state := m.stateFor(p.Id)
	top = append(top, compact(fmt.Sprintf("‹  %d/%d   %s  ›", index+1, len(d.Panels), p.Title), selectedStyle))
	var config settings
	_ = json.Unmarshal([]byte(p.SettingsJson), &config)
	status := "Loading"
	if state != nil && state.loaded {
		status = fmt.Sprintf("%d rows", len(state.result.Rows))
	}
	if state != nil && state.err != nil {
		status = "QUERY ERROR"
	}
	failed := 0
	for _, p := range d.Panels {
		if s := m.stateFor(p.Id); s != nil && s.err != nil {
			failed++
		}
	}
	label := strings.ReplaceAll(string(p.DisplayType), "_", " ") + "   ·   " + status
	if config.Unit != "" {
		label += "   ·   " + config.Unit
	}
	if failed > 0 {
		label += fmt.Sprintf("   ·   %d failed", failed)
	}
	if m.inspect {
		label += "   ·   INSPECTION"
	}
	top = append(top, compact(label, mutedStyle))
	var body []uiLine
	if m.inspect {
		body = append(body, ui("SQL", captionStyle))
		for _, line := range strings.Split(panelSQL(p.QuerySql, m.live), "\n") {
			body = append(body, ui(line, mutedStyle))
		}
		body = append(body, plain(""), ui("Query result", captionStyle))
		if m.err != nil {
			body = append(body, ui(readableError(m.err), errorStyle))
		}
		body = append(body, ui("Scope: "+m.session+" · server active: "+m.activeID, secondaryStyle))
	}
	if state == nil || (!state.loaded && state.err == nil) {
		body = append(body, ui("Loading query results…", secondaryStyle))
		return m.compose(top, body, 0)
	}
	if state.err != nil {
		body = append(body, ui("Query error:", errorStyle))
		for _, line := range strings.Split(readableError(state.err), "\n") {
			body = append(body, ui(line, errorStyle))
		}
		if state.loaded {
			body = append(body, ui("STALE · last successful result:", warningStyle))
		}
	}
	if state.pending {
		body = append(body, ui("STALE · durable changes pending repair", warningStyle))
	}
	if state.loaded {
		if m.inspect {
			for _, line := range renderTable(state.result, string(p.DisplayType) == "time_series", m.width) {
				body = append(body, plain(line))
			}
		} else {
			if telemetryKind(string(p.DisplayType)) {
				for _, warning := range state.result.Warnings {
					body = append(body, ui("Warning: "+warning, warningStyle))
				}
				if state.result.Truncated != nil && *state.result.Truncated {
					body = append(body, ui("Truncated: first 128 result rows; refine query/window.", warningStyle))
				}
				rows, selected := m.telemetry()
				return m.telemetryView(top, body, rows, selected)
			}
			if string(p.DisplayType) == "single_value" {
				body = append(body, plain(""), plain(""))
			}
			chartHeight := max(4, m.height-len(top)-3)
			rendered := renderPanel(string(p.DisplayType), p.SettingsJson, state.result, m.width, chartHeight, m.live)
			for i, line := range rendered {
				if strings.HasPrefix(line, "Unit: ") {
					continue
				} // Unit is beside panel status.
				style := lipgloss.NewStyle()
				switch string(p.DisplayType) {
				case "bar", "pie", "entity_list", "gauge", "heatmap":
					style = chartStyle
				case "single_value":
					style = valueStyle
				case "time_series":
					style = chartStyle
					if strings.HasPrefix(line, "Series:") {
						style = captionStyle
					} else if strings.Contains(line, " → ") {
						style = secondaryStyle
					}
				case "table":
					if i < 3 {
						style = captionStyle
					}
				}
				if strings.HasPrefix(line, "Warning:") || strings.HasPrefix(line, "Truncated:") || strings.HasPrefix(line, "Invalid ") || strings.HasPrefix(line, "Out of range:") || strings.Contains(line, "null/invalid") || strings.Contains(line, "unsupported in the terminal") {
					style = warningStyle
				}
				if strings.HasPrefix(line, "No ") {
					style = secondaryStyle
				}
				body = append(body, ui(line, style))
			}
		}
	}
	return m.compose(top, body, 0)
}
func (m *model) listView(top []uiLine) string {
	files := 0
	for _, d := range m.rows {
		if d.SourceFile != "" {
			files++
		}
	}
	top = append(top, compact("Dashboards", titleStyle), compact(fmt.Sprintf("%d views   ·   %d file-managed   ·   Read-only", len(m.rows), files), mutedStyle), plain(""))
	if len(m.rows) == 0 {
		return m.compose(top, []uiLine{ui("No saved or file-managed dashboards.", secondaryStyle)}, 0)
	}
	var body []uiLine
	selectedEnd := 0
	for _, d := range m.rows {
		selected := d.Id == m.selected
		if selected {
			selectedEnd = len(wrapUI(body, m.width))
		}
		style := titleStyle
		marker := "  "
		if selected {
			style = selectedStyle
			marker = "› "
		}
		body = append(body, ui(marker+d.Name, style))
		source := "Saved"
		if d.SourceFile != "" {
			source = "File"
		}
		updated := time.Unix(0, d.UpdatedAt).Local().Format("Jan 02 15:04 MST")
		body = append(body, ui(fmt.Sprintf("  %s   ·   %d panels   ·   %d variables   ·   %s", source, len(d.Panels), len(d.Variables), updated), mutedStyle))
		if selected {
			if d.Description != "" {
				body = append(body, ui("  "+d.Description, secondaryStyle))
			}
			if d.SourceFile != "" {
				body = append(body, ui("  "+d.SourceFile, secondaryStyle))
			}
		}
		body = append(body, plain(""))
	}
	return m.compose(top, body, selectedEnd)
}
func (m *model) inputView(top []uiLine, d *apigen.Dashboard) string {
	if len(d.Variables) == 0 {
		return m.compose(top, []uiLine{ui("Variable no longer exists.", errorStyle)}, 0)
	}
	v := d.Variables[m.variable]
	context := string(v.Kind)
	if v.Source != "" {
		context += " · " + v.Source
	}
	body := []uiLine{plain(""), ui("Set $"+v.Name, titleStyle), ui(context, secondaryStyle), plain(""), ui("Value", captionStyle), ui(m.draft+"█", valueStyle), plain(""), ui("Default: "+v.DefaultValue, mutedStyle)}
	if v.OptionsJson != "" && v.OptionsJson != "[]" {
		var options []string
		if json.Unmarshal([]byte(v.OptionsJson), &options) == nil {
			body = append(body, ui("Options: "+strings.Join(options, " · "), mutedStyle))
		} else {
			body = append(body, ui("Options: "+v.OptionsJson, warningStyle))
		}
	}
	if m.inputError != "" {
		body = append(body, ui(m.inputError, errorStyle))
	}
	return m.compose(top, body, 0)
}
