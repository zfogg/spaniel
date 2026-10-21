package main

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type testUIHub struct {
	visible bool
	events  []*ws.Event
}

func (h *testUIHub) HasViewers() bool      { return h.visible }
func (h *testUIHub) Broadcast(e *ws.Event) { h.events = append(h.events, e) }

type testUIRates struct{ calls int }

func (r *testUIRates) Throughput() storage.Throughput {
	r.calls++
	return storage.Throughput{SpansPerSec: 12, LogsPerSec: 3}
}

func TestUIBroadcasterOnlyWorksForViewers(t *testing.T) {
	hub := &testUIHub{}
	rates := &testUIRates{}
	broadcastUIState(hub, rates, nil)
	if rates.calls != 0 || len(hub.events) != 0 {
		t.Fatal("UI bookkeeping ran without viewers")
	}
	hub.visible = true
	broadcastUIState(hub, rates, nil)
	if rates.calls != 1 || len(hub.events) != 1 {
		t.Fatal("visible viewer did not receive throughput")
	}
	payload := hub.events[0].Payload.(*ws.ThroughputPayload)
	if payload.SpansPerSec != 12 || payload.LogsPerSec != 3 {
		t.Fatalf("wrong ingest rates: %+v", payload)
	}
}
