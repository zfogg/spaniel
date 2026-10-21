package main

import (
	"context"
	"time"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/live"
	"github.com/zfogg/spaniel/internal/ws"
)

type uiBroadcaster interface {
	HasViewers() bool
	Broadcast(*ws.Event)
}

func broadcastUILoop(ctx context.Context, hub uiBroadcaster, tp live.ThroughputProvider, fwd *forwarder.Forwarder) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			broadcastUIState(hub, tp, fwd)
		}
	}
}

func broadcastUIState(hub uiBroadcaster, tp live.ThroughputProvider, fwd *forwarder.Forwarder) {
	if !hub.HasViewers() {
		return
	}
	// Use the ingestion counters instead of scanning database row counts every
	// two seconds. Pruning old rows must not change the measured ingest rate.
	rates := tp.Throughput()
	hub.Broadcast(ws.NewThroughputEvent(&ws.ThroughputPayload{SpansPerSec: rates.SpansPerSec, LogsPerSec: rates.LogsPerSec}))
	if fwd != nil {
		for _, s := range fwd.Status() {
			hub.Broadcast(ws.NewForwarderEvent(&ws.ForwarderPayload{URL: s.URL, Sent: s.Sent, Errors: s.Errors, LastErr: s.LastErr, PendingBytes: s.PendingBytes, DroppedSpool: s.DroppedSpool}))
		}
	}
}
