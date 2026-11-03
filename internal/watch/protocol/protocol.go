// Package protocol is the dependency boundary between independent feature tabs,
// the shared detail viewer, and the shell. It contains no feature rendering.
package protocol

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"time"
)

type TabID string

const (
	Traces     TabID = "traces"
	Spans      TabID = "spans"
	Metrics    TabID = "metrics"
	Logs       TabID = "logs"
	Lints      TabID = "lints"
	Dashboards TabID = "dashboards"
	Alerts     TabID = "alerts"
)

var Order = []TabID{Traces, Spans, Metrics, Logs, Lints, Dashboards, Alerts}

type Filters struct {
	Service            string
	ErrorsOnly, N1Only bool
}
type Env struct {
	Context context.Context
	API     *client.Client
	Refresh *client.Refreshes
	Owner   string
	Filters Filters
	Time    *Clock
}
type Factory func(Env) tea.Model

// Owned routes non-HTTP async work (for example widget timers) to its creator.
// HTTP work uses Result and Refreshes so it also receives generation guards.
type Owned struct {
	Owner   string
	Message tea.Msg
}

func (env Env) Command(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return Owned{Owner: env.Owner, Message: cmd()} }
}

type Tab struct {
	ID    TabID
	Title string
	New   Factory
}

type Live struct{ Event liveevent.Message }
type Connection = client.Status
type Result = client.Result
type SessionChanged struct{ ID string }
type Focus struct{ Active bool }
type Tick struct{ Time time.Time }

// KeyCapture lets focused text inputs consume q, escape, and tab. Ctrl+C is
// always global. Feature models handle enter/filter/pause/scroll themselves.
type KeyCapture interface{ CapturesKeys() bool }
type ReturnContext struct {
	Tab       TabID
	Selection string
}
type OpenTrace struct {
	TraceID, SpanID string
	Return          ReturnContext
}
type OpenSpan struct {
	TraceID, SpanID string
	Return          ReturnContext
}
type ShowDetail struct {
	New    Factory
	Return ReturnContext
}
type Back struct{}

// Details is implemented by the traces package; logs/lints/alerts emit typed
// navigation without importing trace or span feature code.
type Details interface {
	Trace(Env, OpenTrace) tea.Model
	Span(Env, OpenSpan) tea.Model
}
