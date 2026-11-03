package lints

import keys "github.com/charmbracelet/bubbles/key"

func binding(press []string, label, action string) keys.Binding {
	return keys.NewBinding(keys.WithKeys(press...), keys.WithHelp(label, action))
}

// ShortHelp is the structural footer contract consumed by the integration
// shell. Keep these actions separate from data/status rendering.
func (m *model) ShortHelp() []keys.Binding {
	if m.editing {
		return []keys.Binding{binding([]string{"enter"}, "enter", "apply filter"), binding([]string{"esc"}, "esc", "cancel filter")}
	}
	if m.detail != nil {
		help := []keys.Binding{}
		if len(m.detailBody()) > m.detailHeight() {
			help = append(help, binding([]string{"up", "down", "k", "j"}, "↑/↓", "scroll"), binding([]string{"pgup", "pgdown"}, "pgup/pgdn", "scroll page"))
		}
		help = append(help, m.navigationHelp()...)
		return help
	}
	help := []keys.Binding{binding([]string{"/"}, "/", "filter")}
	if m.filter != "" {
		help = append(help, binding([]string{"c"}, "c", "clear filter"))
	}
	if len(m.visible) > 0 {
		if len(m.visible) > 1 {
			help = append(help, binding([]string{"up", "down", "k", "j"}, "↑/↓", "select"))
		}
		help = append(help, binding([]string{"enter"}, "enter", "inspect finding"))
		if len(m.visible) > m.pageSize() {
			help = append(help, binding([]string{"left", "right", "pgup", "pgdown"}, "←/→", "page"), binding([]string{"home", "end"}, "home/end", "first/last finding"))
		}
	}
	return help
}

func (m *model) navigationHelp() []keys.Binding {
	var help []keys.Binding
	if m.navigate("t") != nil {
		press, label := []string{"t"}, "t"
		if m.detail.span == "" {
			press = append(press, "enter")
			label = "t/enter"
		}
		help = append(help, binding(press, label, "open trace"))
	}
	if m.navigate("s") != nil {
		help = append(help, binding([]string{"s", "enter"}, "s/enter", "open span"))
	}
	return help
}
