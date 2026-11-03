// Package spans exposes the individual-span tab using the shared trace renderer.
package spans

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/tabs/traces"
)

// New is the stable shell constructor.
func New(env protocol.Env) tea.Model { return traces.NewSpanList(env) }
