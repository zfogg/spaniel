package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestViewerVisibilityKeepsBackgroundAlertsConnected(t *testing.T) {
	h := NewHub()
	if h.HasViewers() {
		t.Fatal("empty hub has viewers")
	}
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.CloseNow()
	waitClients(t, h, 1)
	if !h.HasViewers() {
		t.Fatal("existing client was not compatible")
	}
	setVisibility := func(visible bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"type": "visibility", "visible": visible})
		if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for h.HasViewers() != visible {
			if time.Now().After(deadline) {
				t.Fatal("visibility frame was not applied")
			}
			time.Sleep(time.Millisecond)
		}
	}
	setVisibility(false)
	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "hidden"}))
	h.Broadcast(&Event{Type: "alert", Payload: map[string]string{"ruleId": "important"}})
	var event Event
	if err := json.Unmarshal(readMsg(t, conn), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "alert" {
		t.Fatalf("background socket received %s instead of its alert", event.Type)
	}
	h.Broadcast(&Event{Type: "definition_sync", Payload: map[string]string{"kind": "dashboard", "status": "updated"}})
	if err := json.Unmarshal(readMsg(t, conn), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "definition_sync" {
		t.Fatal("background client missed definition update")
	}
	setVisibility(true)
	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "visible"}))
	if err := json.Unmarshal(readMsg(t, conn), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "span" {
		t.Fatal("foreground span stream did not resume")
	}
}
