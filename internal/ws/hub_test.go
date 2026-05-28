package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial ws: %v", err)
	}
	return conn, srv
}

func TestBroadcastReachesConnectedClient(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.Close()

	ev := NewSpanEvent(&SpanPayload{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "svc",
		Name:        "GET /api/users",
		DurationNs:  1_000_000,
		StatusCode:  0,
	})
	h.Broadcast(ev)

	conn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

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
	defer conn1.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer conn2.Close()

	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "multi"}))

	for _, c := range []*websocket.Conn{conn1, conn2} {
		c.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		var got struct {
			Payload SpanPayload `json:"payload"`
		}
		json.Unmarshal(msg, &got) //nolint:errcheck
		if got.Payload.TraceID != "multi" {
			t.Errorf("expected TraceID=multi, got %q", got.Payload.TraceID)
		}
	}
}

func TestClientRemovedOnDisconnect(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()

	conn.Close()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.clients)
		h.mu.RUnlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("client not removed from hub after disconnect")
}

func TestBroadcast_TypedEvents(t *testing.T) {
	h := NewHub()
	conn, srv := dialHub(t, h)
	defer srv.Close()
	defer conn.Close()

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

		conn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read message %d: %v", i, err)
		}

		var got struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal message %d: %v", i, err)
		}
		if got.Type != expectedTypes[i] {
			t.Errorf("event %d: expected type=%q, got %q", i, expectedTypes[i], got.Type)
		}
	}
}
