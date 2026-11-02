// Package foundation supplies explicitly labeled extension points until the
// feature issues land. No sample rows or duplicate feature renderers live here.
package foundation

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

type Model struct{ owner, title, issue, state string }

func New(env protocol.Env, title, issue string, specs ...client.Spec) tea.Model {
	for _, s := range specs {
		s.Key.Owner = env.Owner
		env.Refresh.Register(s)
	}
	return Model{owner: env.Owner, title: title, issue: issue, state: "Loading stored data…"}
}
func (m Model) Init() tea.Cmd { return nil }
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if r, ok := msg.(protocol.Result); ok && r.Key.Owner == m.owner {
		if r.Err != nil {
			m.state = "Error loading " + r.Key.Name + ": " + r.Err.Error()
		} else {
			m.state = ""
		}
	}
	return m, nil
}
func (m Model) View() string {
	context := m.title + "\n"
	for _, id := range protocol.Order {
		if string(id) == m.title {
			context = ""
			break
		}
	}
	state := ""
	if m.state != "" {
		state = m.state + "\n"
	}
	return fmt.Sprintf("%s%sFoundation extension point — implementation pending %s.", context, state, m.issue)
}
