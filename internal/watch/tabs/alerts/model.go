// Package alerts owns the alerts feature (#131).
package alerts

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/foundation"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"net/url"
)

// New is the stable shell constructor. Replace this labeled foundation model
// within this package when implementing #131.
func New(env protocol.Env) tea.Model {
	return foundation.New(env, "alerts", "#131", client.Spec{
		Key: client.Key{Name: "list"}, Resources: []liveevent.Resource{"alerts"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Alerts(ctx, client.Scope(url.Values{}, session))
		},
	})
}
