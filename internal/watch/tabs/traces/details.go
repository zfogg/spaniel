package traces

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/foundation"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

// NewDetails is the shared trace/span renderer boundary owned by #126.
func NewDetails() protocol.Details { return details{} }

type details struct{}

func (details) Trace(env protocol.Env, target protocol.OpenTrace) tea.Model {
	return foundation.New(env, "trace "+target.TraceID, "#126", client.Spec{Key: client.Key{Name: "trace"}, Resources: []liveevent.Resource{"trace"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Trace(ctx, target.TraceID, client.Scope(nil, session))
	}})
}
func (details) Span(env protocol.Env, target protocol.OpenSpan) tea.Model {
	return foundation.New(env, "span "+target.SpanID, "#126", client.Spec{Key: client.Key{Name: "span"}, Resources: []liveevent.Resource{"span"}, Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
		return api.Span(ctx, target.SpanID, client.Scope(nil, session))
	}})
}
