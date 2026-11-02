package traces

import "github.com/charmbracelet/bubbles/key"

func binding(keys []string, display, action string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(display, action))
}
func reloadHelp(state viewState) key.Binding {
	action := "reload"
	if state.err != "" {
		action = "retry"
	}
	return binding([]string{"r"}, "r", action)
}

// ShortHelp is a structural extension point for the coordinator-owned footer.
// Global tab/Back/quit bindings belong to the shell, not individual features.
func (m *listModel) ShortHelp() []key.Binding {
	if m.input != "" {
		return []key.Binding{binding([]string{"enter"}, "enter", "apply "+m.input+" filter"), binding([]string{"esc"}, "esc", "cancel"), binding([]string{"backspace", "ctrl+h"}, "backspace", "erase")}
	}
	var help []key.Binding
	if len(m.visible()) > 0 {
		help = append(help, binding([]string{"up", "down", "j", "k"}, "↑/↓", "select"), binding([]string{"pgup", "pgdown"}, "PgUp/PgDn", "scroll"), binding([]string{"home", "end"}, "Home/End", "first/last"), binding([]string{"enter"}, "enter", "open"))
	}
	if m.page > 1 {
		help = append(help, binding([]string{"[", "left"}, "[", "previous page"))
	}
	if m.hasNext() {
		help = append(help, binding([]string{"]", "right"}, "]", "next page"))
	}
	errorAction, n1Action, pauseAction := "errors only (page)", "N+1 only (page)", "pause"
	if m.errors {
		errorAction = "all statuses (page)"
	}
	if m.n1 {
		n1Action = "all findings (page)"
	}
	if m.paused {
		pauseAction = "resume"
	}
	return append(help, binding([]string{"/"}, "/", "text filter (page)"), binding([]string{"s"}, "s", "service filter"), binding([]string{"e"}, "e", errorAction), binding([]string{"n"}, "n", n1Action), binding([]string{"p"}, "p", pauseAction), reloadHelp(m.state))
}
func (m *traceModel) ShortHelp() []key.Binding {
	var help []key.Binding
	if len(m.rows()) > 0 {
		help = append(help, binding([]string{"up", "down", "j", "k"}, "↑/↓", "select span"), binding([]string{"pgup", "pgdown"}, "PgUp/PgDn", "scroll"), binding([]string{"home", "end"}, "Home/End", "first/last"), binding([]string{"enter"}, "enter", "inspect span"))
	}
	pause := "pause"
	if m.paused {
		pause = "resume"
	}
	return append(help, binding([]string{"p"}, "p", pause), reloadHelp(m.state))
}
func (m *spanModel) ShortHelp() []key.Binding {
	var help []key.Binding
	if m.span != nil {
		help = append(help, binding([]string{"up", "down", "j", "k"}, "↑/↓", "scroll"), binding([]string{"pgup", "pgdown", " "}, "PgUp/PgDn", "page"), binding([]string{"home", "end"}, "Home/End", "first/last"))
	}
	if m.traceID != "" {
		help = append(help, binding([]string{"t"}, "t", "containing trace"))
	}
	return append(help, reloadHelp(m.state))
}
