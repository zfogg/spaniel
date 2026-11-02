// Package watch composes the seven independent terminal features once.
package watch

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/shell"
	"github.com/zfogg/spaniel/internal/watch/tabs/alerts"
	"github.com/zfogg/spaniel/internal/watch/tabs/dashboards"
	"github.com/zfogg/spaniel/internal/watch/tabs/lints"
	"github.com/zfogg/spaniel/internal/watch/tabs/logs"
	"github.com/zfogg/spaniel/internal/watch/tabs/metrics"
	"github.com/zfogg/spaniel/internal/watch/tabs/spans"
	"github.com/zfogg/spaniel/internal/watch/tabs/traces"
)

type Options struct {
	SessionID string
	Filters   protocol.Filters
}

func New(ctx context.Context, api *client.Client, options Options) (*shell.Model, error) {
	updates, err := api.Stream(ctx)
	if err != nil {
		return nil, err
	}
	refresh := client.NewRefreshes(ctx, api)
	refresh.SetSession(options.SessionID)
	if options.SessionID == "" {
		refresh.Register(client.Spec{Key: client.Key{Owner: "$shell", Name: "active-session"}, Resources: []liveevent.Resource{"active-session"}, Fetch: func(ctx context.Context, api *client.Client, _ string) (any, error) {
			return api.ActiveSession(ctx, nil)
		}})
	}
	env := protocol.Env{Context: ctx, API: api, Refresh: refresh, Filters: options.Filters}
	tabs := []protocol.Tab{
		{ID: protocol.Traces, Title: "traces", New: traces.New},
		{ID: protocol.Spans, Title: "spans", New: spans.New},
		{ID: protocol.Metrics, Title: "metrics", New: metrics.New},
		{ID: protocol.Logs, Title: "logs", New: logs.New},
		{ID: protocol.Lints, Title: "lints", New: lints.New},
		{ID: protocol.Dashboards, Title: "dashboards", New: dashboards.New},
		{ID: protocol.Alerts, Title: "alerts", New: alerts.New},
	}
	return shell.New(env, tabs, traces.NewDetails(), updates, options.SessionID != ""), nil
}
func Run(ctx context.Context, base string, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	api, err := client.New(base, client.Options{})
	if err != nil {
		return err
	}
	model, err := New(ctx, api, options)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}
