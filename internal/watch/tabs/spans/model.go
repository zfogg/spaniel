// Package spans owns the spans feature (#126).
package spans

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
// within this package when implementing #126.
func New(env protocol.Env) tea.Model {
	return foundation.New(env, "spans", "#126", client.Spec{
		Key: client.Key{Name: "list"}, Resources: []liveevent.Resource{"spans"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Spans(ctx, client.Scope(url.Values{}, session))
		},
	})
}
