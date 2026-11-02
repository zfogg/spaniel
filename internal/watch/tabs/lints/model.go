// Package lints owns the lints feature (#129).
package lints

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
// within this package when implementing #129.
func New(env protocol.Env) tea.Model {
	env.Refresh.Register(client.Spec{Key: client.Key{Owner: env.Owner, Name: "issues"}, Resources: []liveevent.Resource{"issues"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Issues(ctx, client.Scope(nil, session))
	}})
	return foundation.New(env, "lints", "#129", client.Spec{
		Key: client.Key{Name: "list"}, Resources: []liveevent.Resource{"lint", "issues"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Lints(ctx, client.Scope(url.Values{}, session))
		},
	})
}
