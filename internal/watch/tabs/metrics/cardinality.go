package metrics

import (
	"context"
	"fmt"
	"net/url"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

// Cardinality comes from the durable admission catalog, independently of the
// selected chart window, filtered samples, or locally patched label sets.
type admission struct {
	streams         map[string]apigen.MetricCardinalityStream
	loaded, pending bool
	err             error
}

func registerAdmission(env protocol.Env) {
	env.Refresh.Register(client.Spec{Key: client.Key{Owner: env.Owner, Name: "cardinality"}, Resources: []liveevent.Resource{"metrics"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.MetricCardinality(ctx, client.Scope(url.Values{}, session))
		},
	})
}

func (a *admission) accept(result protocol.Result, owner, session string) bool {
	if result.Key != (client.Key{Owner: owner, Name: "cardinality"}) {
		return false
	}
	if result.Session != session {
		return true
	}
	a.err, a.pending = result.Err, result.Pending
	if result.Err == nil {
		if value, ok := result.Data.(client.Envelope[[]apigen.MetricCardinalityStream]); ok {
			a.loaded = true
			a.streams = make(map[string]apigen.MetricCardinalityStream, min(len(value.Data), catalogLimit))
			for _, row := range value.Data[:min(len(value.Data), catalogLimit)] {
				a.streams[row.ServiceName+"\x00"+row.Name] = row
			}
		}
	}
	return true
}

func (a admission) summary(service, name string) string {
	text := "Admission: loading durable series budget…"
	if a.loaded {
		text = "Admission: no durable budget record returned for this stream"
		if row, ok := a.streams[service+"\x00"+name]; ok {
			text = fmt.Sprintf("Admission: %s active series / %s limit (recording scope)", number(row.ActiveSeries), number(row.Limit))
		}
	}
	if a.err != nil {
		text += " · unavailable/stale: " + safe(a.err.Error())
	} else if a.pending {
		text += " · stale; repair pending"
	}
	return text
}
