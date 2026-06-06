package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNewHub(t *testing.T) {
	h := NewHub()
	if h == nil {
		t.Fatal("NewHub returned nil")
	}
	if h.clients == nil {
		t.Fatal("clients map not initialized")
	}
}

func TestBroadcastNoClients(t *testing.T) {
	h := NewHub()
	// Must not panic with zero clients.
	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "abc", SpanID: "def"}))
}

func dialHub(t *testing.T, h *Hub) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial ws: %v", err)
	}
	return conn, srv
}

// readMsg reads one non-heartbeat message with a 1s deadline, failing the test
// on error. Heartbeat frames are ambient (every heartbeatInterval) and skipped.
func readMsg(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read message: %v", err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg, &probe) == nil && probe.Type == "heartbeat" {
			continue
		}
		return msg
	}
}

// waitClients blocks until the hub reports exactly n clients, or fails after
// 500ms.
func waitClients(t *testing.T, h *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		got := len(h.clients)
		h.mu.RUnlock()
		if got == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.RLock()
	got := len(h.clients)
	h.mu.RUnlock()
	t.Fatalf("expected %d clients, got %d", n, got)
}

func TestBroadcastReachesConnectedClient(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.CloseNow() //nolint:errcheck

	// Dial returns once the handshake completes, which can race the server's
	// client registration; wait for it before broadcasting.
	waitClients(t, h, 1)

	ev := NewSpanEvent(&SpanPayload{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "svc",
		Name:        "GET /api/users",
		DurationNs:  1_000_000,
		StatusCode:  0,
	})
	h.Broadcast(ev)

	msg := readMsg(t, conn)

	var got struct {
		Type    string      `json:"type"`
		Payload SpanPayload `json:"payload"`
	}
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Payload.TraceID != "trace-1" {
		t.Errorf("expected TraceID=%q, got %q", "trace-1", got.Payload.TraceID)
	}
	if got.Payload.Name != "GET /api/users" {
		t.Errorf("expected Name=%q, got %q", "GET /api/users", got.Payload.Name)
	}
	if got.Type != "span" {
		t.Errorf("expected type=span, got %q", got.Type)
	}
}

func TestBroadcastReachesMultipleClients(t *testing.T) {
	h := NewHub()
	conn1, srv := dialHub(t, h)
	defer srv.Close()
	defer conn1.CloseNow() //nolint:errcheck

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn2, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer conn2.CloseNow() //nolint:errcheck

	// Wait for both clients to register before broadcasting.
	waitClients(t, h, 2)

	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "multi"}))

	for _, c := range []*websocket.Conn{conn1, conn2} {
		var got struct {
			Payload SpanPayload `json:"payload"`
		}
		json.Unmarshal(readMsg(t, c), &got) //nolint:errcheck
		if got.Payload.TraceID != "multi" {
			t.Errorf("expected TraceID=multi, got %q", got.Payload.TraceID)
		}
	}
}

func TestClientRemovedOnDisconnect(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()

	conn.CloseNow() //nolint:errcheck

	waitClients(t, h, 0)
}

func TestBroadcast_TypedEvents(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.CloseNow() //nolint:errcheck

	waitClients(t, h, 1)

	events := []*Event{
		NewSpanEvent(&SpanPayload{TraceID: "t1", SpanID: "s1"}),
		NewLogEvent(&LogPayload{Body: "hello log", TraceID: "t2"}),
		NewMetricEvent(&MetricPayload{Name: "http.requests", Value: 1.0}),
		NewIssueEvent(&IssuePayload{TraceID: "t3", Kind: "n_plus_one"}),
		NewForwarderEvent(&ForwarderPayload{URL: "http://upstream:4318", Sent: 5}),
		NewThroughputEvent(&ThroughputPayload{SpansPerSec: 3.5, LogsPerSec: 1.2}),
	}

	expectedTypes := []string{"span", "log", "metric", "issue", "forwarder", "throughput"}

	for i, ev := range events {
		h.Broadcast(ev)

		var got struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(readMsg(t, conn), &got); err != nil {
			t.Fatalf("unmarshal message %d: %v", i, err)
		}
		if got.Type != expectedTypes[i] {
			t.Errorf("event %d: expected type=%q, got %q", i, expectedTypes[i], got.Type)
		}
	}
}

// TestHeartbeat verifies the server emits periodic heartbeat frames so clients
// can detect a live (or dead) connection.
func TestHeartbeat(t *testing.T) {
	h := NewHub()
	h.heartbeatInterval = 30 * time.Millisecond // fast for the test
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.CloseNow() //nolint:errcheck

	waitClients(t, h, 1)

	// Read raw frames (don't skip heartbeats) and expect at least one heartbeat.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, msg, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg, &probe) == nil && probe.Type == "heartbeat" {
			return // success
		}
	}
	t.Fatal("no heartbeat frame received within 1s")
}

// TestSlowClientDropped verifies that a client which never reads is dropped
// (its send buffer fills) instead of blocking Broadcast forever.
func TestSlowClientDropped(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.CloseNow() //nolint:errcheck

	waitClients(t, h, 1)

	// The client never reads. Flood far past the send buffer; Broadcast must not
	// block, and the hub must eventually drop the unresponsive client.
	done := make(chan struct{})
	go func() {
		for range clientSendBuffer * 4 {
			h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "flood"}))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked on a slow client")
	}

	waitClients(t, h, 0)
}
