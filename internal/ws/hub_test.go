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
	h.Broadcast(&SpanEvent{Type: "span", TraceID: "abc", SpanID: "def"})
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

	ev := &SpanEvent{
		Type:        "span",
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "svc",
		Name:        "GET /api/users",
		DurationNs:  1_000_000,
		StatusCode:  0,
	}
	h.Broadcast(ev)

	conn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	var got SpanEvent
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TraceID != ev.TraceID {
		t.Errorf("expected TraceID=%q, got %q", ev.TraceID, got.TraceID)
	}
	if got.Name != ev.Name {
		t.Errorf("expected Name=%q, got %q", ev.Name, got.Name)
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

	h.Broadcast(&SpanEvent{Type: "span", TraceID: "multi"})

	for _, c := range []*websocket.Conn{conn1, conn2} {
		c.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		var got SpanEvent
		json.Unmarshal(msg, &got) //nolint:errcheck
		if got.TraceID != "multi" {
			t.Errorf("expected TraceID=multi, got %q", got.TraceID)
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
