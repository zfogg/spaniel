package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestConcurrentBroadcast(t *testing.T) {
	h := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")

	const numClients = 3
	conns := make([]*websocket.Conn, numClients)
	for i := range numClients {
		c, _, err := websocket.Dial(context.Background(), wsBase, nil)
		if err != nil {
			t.Fatalf("dial client %d: %v", i, err)
		}
		conns[i] = c
		defer c.CloseNow() //nolint:errcheck
	}

	// Wait for the hub to register all clients.
	waitClients(t, h, numClients)

	const numBroadcasts = 10
	var wg sync.WaitGroup
	wg.Add(numBroadcasts)
	for i := range numBroadcasts {
		idx := i
		go func() {
			defer wg.Done()
			h.Broadcast(NewSpanEvent(&SpanPayload{
				TraceID: strings.Repeat("a", idx+1),
			}))
		}()
	}
	wg.Wait()

	for _, c := range conns {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_, _, err := c.Read(ctx)
			cancel()
			if err != nil {
				// Timeout or close is expected once messages are drained.
				break
			}
		}
	}
}

func TestBroadcastAfterAllClientsGone(t *testing.T) {
	h := NewHub()

	conn, srv := dialHub(t, h)
	defer srv.Close()

	conn.CloseNow() //nolint:errcheck

	waitClients(t, h, 0)

	h.Broadcast(NewSpanEvent(&SpanPayload{TraceID: "after-gone"}))
}
