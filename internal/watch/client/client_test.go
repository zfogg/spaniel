package client

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/zfogg/spaniel/internal/liveevent"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func server(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(h)
	s.Listener.Close()
	s.Listener = ln
	s.Start()
	t.Cleanup(s.Close)
	return s
}
func next(t *testing.T, ch <-chan Update) Update {
	t.Helper()
	select {
	case u, ok := <-ch:
		if !ok {
			t.Fatal("unexpected stream close")
		}
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("stream timeout")
		return Update{}
	}
}
func TestInitialFailureReconnectAndCancellation(t *testing.T) {
	var dials atomic.Int32
	srv := server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := dials.Add(1)
		if attempt == 1 {
			http.Error(w, "offline", 503)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		b, _ := json.Marshal(liveevent.NewHeartbeatEvent())
		conn.Write(ctx, websocket.MessageText, b)
		if attempt == 2 {
			b, _ = json.Marshal(liveevent.NewSpanEvent(&liveevent.SpanPayload{TraceID: "seed", SpanID: "one"}))
			conn.Write(ctx, websocket.MessageText, b)
			return
		}
		conn.Read(ctx)
	}))
	c, _ := New(srv.URL, Options{MinBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := c.Stream(ctx)
	if _, err := c.Stream(ctx); err == nil {
		t.Fatal("allowed a second socket")
	}
	if u := next(t, ch); u.Status == nil || u.Status.Err == nil {
		t.Fatal("initial failure missing")
	}
	var epochs []uint64
	gotSpan := false
	for len(epochs) < 2 {
		u := next(t, ch)
		if u.Status != nil && u.Status.Connected {
			if !u.Status.Resync {
				t.Fatal("missing resync")
			}
			epochs = append(epochs, u.Status.Epoch)
		}
		if u.Event != nil && u.Event.Type == "span" {
			gotSpan = true
		}
	}
	if epochs[1] <= epochs[0] || !gotSpan {
		t.Fatalf("reconnect lost state: %v, span %v", epochs, gotSpan)
	}
	cancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("stream leaked after cancellation")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() {
			for range ch {
			}
			close(done)
		}()
		return done
	}():
	}
}
func TestQueueOverflowAndStaleConnection(t *testing.T) {
	for _, overflow := range []bool{true, false} {
		t.Run(map[bool]string{true: "overflow", false: "stale"}[overflow], func(t *testing.T) {
			wrote := make(chan struct{}, 1)
			srv := server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				defer cancel()
				b, _ := json.Marshal(liveevent.NewHeartbeatEvent())
				for i := 0; i < 10; i++ {
					conn.Write(ctx, websocket.MessageText, b)
					if !overflow {
						break
					}
				}
				select {
				case wrote <- struct{}{}:
				default:
				}
				conn.Read(ctx)
			}))
			c, _ := New(srv.URL, Options{Queue: 1, StaleAfter: 30 * time.Millisecond, MinBackoff: time.Second})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch, _ := c.Stream(ctx)
			if overflow {
				<-wrote
			}
			connected, disconnected := false, false
			for !disconnected {
				u := next(t, ch)
				if u.Status != nil {
					if u.Status.Connected {
						connected = true
					} else {
						disconnected = true
						if u.Status.Err == nil {
							t.Fatal("no failure reason")
						}
						if overflow && !strings.Contains(u.Status.Err.Error(), "overflow") {
							t.Fatal(u.Status.Err)
						}
					}
				}
			}
			if !connected {
				t.Fatal("no connected status")
			}
			cancel()
			for range ch {
			}
		})
	}
}
func TestTypedHTTPPrecisionScopeErrorsAndTimeout(t *testing.T) {
	const ns int64 = 1793631000123456789
	srv := server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/spans/missing" {
			http.Error(w, "gone", 404)
			return
		}
		if r.URL.Path == "/api/logs" {
			<-r.Context().Done()
			return
		}
		if r.URL.Query().Get("sessionId") != "session" {
			t.Error("session not scoped")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"trace_id": "seed", "start_ns": ns}}, "meta": map[string]int{"total": 1}})
	}))
	c, _ := New(srv.URL, Options{Timeout: 30 * time.Millisecond})
	q := url.Values{"service": {"api"}}
	out, err := c.Traces(context.Background(), Scope(q, "session"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Data[0].StartNs != ns {
		t.Fatalf("timestamp lost precision: %v", out.Data[0].StartNs)
	}
	if q.Get("sessionId") != "" {
		t.Fatal("scope mutated shared filters")
	}
	_, err = c.Span(context.Background(), "missing", nil)
	var apiErr *HTTPError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("untyped error: %v", err)
	}
	_, err = c.Logs(context.Background(), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded HTTP request: %v", err)
	}
}
