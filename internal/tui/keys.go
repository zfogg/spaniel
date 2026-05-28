package tui

import "github.com/charmbracelet/bubbles/key"

// Keys is the shared key.Binding set used across every TUI screen. Defining
// them centrally means the footer help text and the actual handlers can't
// drift — every screen renders the same hint for the same action.
var Keys = struct {
	Quit     key.Binding
	Help     key.Binding
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
	Enter    key.Binding
	Filter   key.Binding
	Pause    key.Binding
	Follow   key.Binding
	Copy     key.Binding
	Clear    key.Binding
	Tab      key.Binding
	ShiftTab key.Binding
	Rerun    key.Binding
}{
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "f", " "), key.WithHelp("pgdn", "page down")),
	Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Pause:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Follow:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "follow")),
	Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
	Clear:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
	Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
	ShiftTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev pane")),
	Rerun:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rerun")),
}
