// Package live publishes small derived UI snapshots when durable telemetry
// changes. It is intentionally server-owned: browsers never choose its cadence.
package live

import (
	"context"
	"sync"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/otel/codes"
)

const coalesceDelay = 100 * time.Millisecond

type ThroughputProvider interface{ Throughput() storage.Throughput }

type DropCounterProvider interface {
	DroppedSpansTotal() int64
	DroppedLogsTotal() int64
	DroppedMetricPointsTotal() int64
	LastDropAt() int64
}

// Publisher coalesces ingest completions. After traffic stops it continues only
// while the ten-second rolling throughput calculation is changing, so the live
// indicator naturally returns to idle without a browser poller.
type Publisher struct {
	store *storage.DB
	hub   *ws.Hub
	tp    ThroughputProvider
	dc    DropCounterProvider

	mu    sync.Mutex
	timer *time.Timer
}

func NewPublisher(store *storage.DB, hub *ws.Hub, tp ThroughputProvider, dc DropCounterProvider) *Publisher {
	return &Publisher{store: store, hub: hub, tp: tp, dc: dc}
}

// Notify is safe to call after any successful durable ingest.
func (p *Publisher) Notify() {
	if !p.hub.HasViewers() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer == nil {
		p.timer = time.AfterFunc(coalesceDelay, p.publish)
	}
}

func (p *Publisher) publish() {
	if !p.hub.HasViewers() {
		p.mu.Lock()
		p.timer = nil
		p.mu.Unlock()
		return
	}
	sessionID := p.store.ActiveSessionID()
	ctx, span := telemetry.Tracer("live").Start(context.Background(), "live.publish")
	defer span.End()
	store := p.store.WithContext(ctx)
	stats, err := store.GetStats(sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read live stats")
	}
	if err == nil {
		if p.tp != nil {
			stats.Throughput = p.tp.Throughput()
		}
		if p.dc != nil {
			stats.DroppedSpans = p.dc.DroppedSpansTotal()
			stats.DroppedLogs = p.dc.DroppedLogsTotal()
			stats.DroppedMetricPoints = p.dc.DroppedMetricPointsTotal()
			stats.LastDropAt = p.dc.LastDropAt()
		}
		sources, sourceErr := store.GetSourceStats(sessionID)
		if sourceErr != nil {
			span.RecordError(sourceErr)
			span.SetStatus(codes.Error, "read live sources")
		}
		if sourceErr == nil {
			if sources == nil {
				sources = []storage.SourceStats{}
			}
			p.hub.Broadcast(ws.NewLiveStateEvent(&ws.LiveStatePayload{Stats: stats, Sources: sources}))
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.timer = nil
	if err == nil && stats.Throughput.SpansPerSec+stats.Throughput.LogsPerSec+stats.Throughput.MetricsPerSec > 0 {
		p.timer = time.AfterFunc(time.Second, p.publish)
	}
}
