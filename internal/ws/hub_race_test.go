package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConcurrentBroadcast(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")

	const numClients = 3
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsBase, nil)
		if err != nil {
			t.Fatalf("dial client %d: %v", i, err)
		}
		conns[i] = c
		defer c.Close()
	}

	// Wait briefly for hub to register all clients.
	time.Sleep(50 * time.Millisecond)

	const numBroadcasts = 10
	var wg sync.WaitGroup
	wg.Add(numBroadcasts)
	for i := 0; i < numBroadcasts; i++ {
		idx := i
		go func() {
			defer wg.Done()
			h.Broadcast(NewSpanEvent(&SpanPayload{
				TraceID: strings.Repeat("a", idx+1),
			}))
		}()
	}
	wg.Wait()

	for i, c := range conns {
		c.SetReadDeadline(time.Now().Add(200 * time.Millisecond)) //nolint:errcheck
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				// Timeout or close is expected once messages are drained.
				break
			}
		}
		_ = i
	}
}

func TestBroadcastAfterAllClientsGone(t *testing.T) {
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
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	if n != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", n)
	}

	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "after-gone"}))
}
