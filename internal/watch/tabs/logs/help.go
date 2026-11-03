package logs

import "github.com/charmbracelet/bubbles/key"

func binding(keys []string, label, description string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, description))
}

// ShortHelp is the structural footer interface used by the integration shell.
// Inline legends remain until that shell switches all features atomically.
func (m *model) ShortHelp() []key.Binding {
	if m.CapturesKeys() {
		return []key.Binding{
			binding([]string{"enter"}, "Enter", "apply "+m.editing),
			binding([]string{"esc"}, "Esc", "cancel"),
			binding([]string{"ctrl+u"}, "Ctrl+U", "clear input"),
		}
	}
	var help []key.Binding
	if len(m.visible()) > 0 {
		help = append(help,
			binding([]string{"up", "down", "k", "j"}, "↑↓", "select"),
			binding([]string{"enter"}, "Enter", "full log"),
			binding([]string{"pgup", "pgdown"}, "PgUp/PgDn", "scroll list"),
			binding([]string{"home", "end", "g", "G"}, "Home/End", "first/last log"),
		)
	}
	help = append(help,
		binding([]string{"/"}, "/", "text filter"),
		binding([]string{"s"}, "s", "service filter"),
		binding([]string{"v"}, "v", "cycle severity"),
	)
	if !m.loading && m.page > 1 {
		help = append(help, binding([]string{"["}, "[", "previous page"))
	}
	if !m.loading && m.page*pageSize < m.total {
		help = append(help, binding([]string{"]"}, "]", "next page"))
	}
	pause := "pause rows"
	if m.paused {
		pause = "resume rows"
	}
	return append(help, binding([]string{"p", " "}, "p", pause))
}

func (m *detail) ShortHelp() []key.Binding {
	var help []key.Binding
	if m.view.TotalLineCount() > m.view.Height {
		help = append(help,
			binding([]string{"up", "down", "k", "j"}, "↑↓", "scroll log"),
			binding([]string{"pgup", "pgdown", "f", "b", " "}, "PgUp/PgDn", "page log"),
			binding([]string{"home", "end", "g", "G"}, "Home/End", "top/bottom"),
		)
	}
	if m.changedSession {
		return help
	}
	if m.row.TraceId != "" {
		help = append(help, binding([]string{"enter", "t"}, "Enter/t", "trace with correlated span"))
	}
	if m.row.SpanId != "" {
		if m.row.TraceId == "" {
			help = append(help, binding([]string{"enter", "s"}, "Enter/s", "correlated span"))
		} else {
			help = append(help, binding([]string{"s"}, "s", "correlated span"))
		}
	}
	return help
}
