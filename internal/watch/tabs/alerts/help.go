package alerts

import "github.com/charmbracelet/bubbles/key"

func helpBinding(keys []string, display, description string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(display, description))
}

// ShortHelp exposes only actions applicable to the focused alert view. The
// coordinator's shared footer can discover this structural method without a
// protocol change. Global tab/Back/quit bindings remain the shell's concern.
func (m *model) ShortHelp() []key.Binding {
	if m.editing {
		return []key.Binding{
			helpBinding([]string{"enter"}, "enter", "apply search"),
			helpBinding([]string{"esc"}, "esc", "cancel search"),
			helpBinding([]string{"backspace", "ctrl+h"}, "backspace", "delete character"),
			helpBinding([]string{"ctrl+u"}, "ctrl+u", "clear input"),
		}
	}
	rows := m.id == "" || m.section == "instances" || m.section == "history" || m.section == "links"
	action := "scroll"
	if rows {
		action = "select"
	}
	bindings := []key.Binding{
		helpBinding([]string{"up", "k"}, "↑/k", action+" previous"),
		helpBinding([]string{"down", "j"}, "↓/j", action+" next"),
		helpBinding([]string{"pgup", "pgdown"}, "pgup/pgdn", action+" page"),
		helpBinding([]string{"home", "end"}, "home/end", action+" first/last"),
	}
	if m.id == "" {
		if m.selected != "" {
			bindings = append(bindings, helpBinding([]string{"enter"}, "enter", "open rule"))
		}
	} else {
		switch m.section {
		case "instances":
			if m.instance != "" {
				bindings = append(bindings, helpBinding([]string{"enter"}, "enter", "inspect instance"))
			}
		case "history":
			if m.event != "" {
				bindings = append(bindings, helpBinding([]string{"enter"}, "enter", "inspect event"))
			}
		case "links":
			for _, link := range m.links() {
				if link.key() != m.link {
					continue
				}
				description := "show link status"
				if link.external == "" && link.unavailable == "" && !m.deleted() {
					description = "open telemetry"
				}
				bindings = append(bindings, helpBinding([]string{"enter"}, "enter", description))
				break
			}
		}
		for _, section := range []struct{ key, title string }{{"d", "definition"}, {"i", "instances"}, {"h", "history"}, {"l", "links"}} {
			if m.section != section.title {
				bindings = append(bindings, helpBinding([]string{section.key}, section.key, section.title))
			}
		}
		if m.group != "" {
			bindings = append(bindings, helpBinding([]string{"g"}, "g", "history: all groups"))
		} else if m.instance != "" {
			bindings = append(bindings, helpBinding([]string{"g"}, "g", "history: selected instance"))
		}
		if m.section != "links" && len(m.links()) > 0 {
			bindings = append(bindings, helpBinding([]string{"t"}, "t", "telemetry links"))
		}
	}
	if m.id == "" || m.section == "instances" || m.section == "history" {
		state, severity := m.state, m.severity
		if m.id != "" && m.section == "history" {
			state, severity = m.hstate, m.hseverity
		}
		scope := ""
		if m.id == "" {
			scope = " (page)"
		}
		bindings = append(bindings,
			helpBinding([]string{"/"}, "/", "search"),
			helpBinding([]string{"s"}, "s", "state"+scope+": "+label(state)),
			helpBinding([]string{"c"}, "c", "clear filters"),
		)
		if m.id == "" || m.section == "history" {
			bindings = append(bindings, helpBinding([]string{"v"}, "v", "severity"+scope+": "+label(severity)))
		}
	}
	if m.id == "" || m.section == "history" {
		if m.page > 1 {
			bindings = append(bindings, helpBinding([]string{"["}, "[", "previous API page"))
		}
		if m.page*pageSize < m.total {
			bindings = append(bindings, helpBinding([]string{"]"}, "]", "next API page"))
		}
	}
	return bindings
}
