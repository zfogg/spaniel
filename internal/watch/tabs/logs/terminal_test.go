package logs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/ws"
)

type output struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (o *output) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(b)
}
func (o *output) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.buffer.String() }

// Exercise the production app, HTTP reader, single real hub socket, scheduler,
// hidden detail stack and renderer together. Optional captures contain only
// safe fixture telemetry delivered through HTTP/live events, never mock views.
func TestLogsTerminalLifecycle(t *testing.T) {
	if os.Getenv("SPANIEL_WATCH_CAPTURE_DIR") != "" {
		profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ns := time.Date(2026, 11, 2, 17, 30, 0, 123000000, time.UTC).UnixNano()
	row := apigen.Log{TimestampNs: ns, ReceivedAt: ns + 2000000, ServiceName: "checkout-api", Severity: 9, Body: "Order accepted\nReserved 3 items; payment authorized.\nReceipt queued for delivery.", TraceId: "4bf92f3577b34da6a3ce929d0e0e4736", SpanId: "00f067aa0ba902b7", SessionId: "demo", Attributes: `{"order.id":"demo-1042","cart.items":3,"payment.provider":"sandbox","http.request.method":"POST","retryable":false,"inventory":{"region":"us-east","attempts":[1,2]},"receipt.sequence":18446744073709551615}`}
	warn := row
	warn.TimestampNs -= 1000000000
	warn.ReceivedAt -= 1000000000
	warn.Severity = 13
	warn.Body = "Inventory response slow; retry succeeded"
	warn.ServiceName = "inventory"
	unlinked := row
	unlinked.TimestampNs -= 2000000000
	unlinked.ReceivedAt -= 2000000000
	unlinked.Severity = 5
	unlinked.Body = "Background health check completed"
	unlinked.TraceId, unlinked.SpanId = "", ""
	var mu sync.Mutex
	rows := []apigen.Log{row, warn, unlinked}
	hub := ws.NewHub()
	sockets := make(chan net.Conn, 8)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			hub.ServeWS(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/traces/"+row.TraceId {
			http.Error(w, `{"error":"Trace expired"}`, http.StatusNotFound)
			return
		}
		var data any = []any{}
		if r.URL.Path == "/api/alerts" {
			data = map[string]any{"items": []any{}}
		}
		if r.URL.Path == "/api/logs" {
			if r.URL.Query().Get("sessionId") != "demo" {
				t.Error("unscoped log read")
			}
			mu.Lock()
			copied := append([]apigen.Log(nil), rows...)
			mu.Unlock()
			if r.URL.Query().Get("fromNs") != "" {
				copied = []apigen.Log{row}
			}
			data = copied
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	srv.Listener.Close()
	srv.Listener = listener
	srv.Config.ConnState = func(c net.Conn, s http.ConnState) {
		if s == http.StateHijacked {
			select {
			case sockets <- c:
			default:
			}
		}
	}
	srv.Start()
	defer srv.Close()
	api, _ := client.New(srv.URL, client.Options{MinBackoff: 250 * time.Millisecond, MaxBackoff: time.Second})
	m, err := watch.New(ctx, api, watch.Options{SessionID: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	var rendered output
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&rendered), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	wait := func(text string) {
		t.Helper()
		deadline := time.Now().Add(7 * time.Second)
		for !strings.Contains(rendered.text(), text) {
			if time.Now().After(deadline) {
				t.Fatalf("terminal missing %q: %s", text, rendered.text())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	send := func(typ tea.KeyType) { p.Send(tea.KeyMsg{Type: typ}) }
	capture := func(name string) {
		t.Helper()
		if dir := os.Getenv("SPANIEL_WATCH_CAPTURE_DIR"); dir != "" {
			time.Sleep(100 * time.Millisecond)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(rendered.text()), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for n := 0; n < 3; n++ {
		send(tea.KeyTab)
	}
	wait("Order accepted")
	capture("logs-list")
	send(tea.KeyEnter)
	wait("Full log")
	wait("cart.items")
	capture("logs-detail")
	send(tea.KeyEnd)
	wait("18446744073709551615")
	capture("logs-attributes")
	// An expired shared trace viewer must be escapable back to the retained log.
	send(tea.KeyEnter)
	wait("HTTP 404")
	send(tea.KeyEsc)
	send(tea.KeyTab) // Keep the full log hidden on another tab.
	live := row
	live.TimestampNs += 1000000000
	live.ReceivedAt += 1000000000
	live.Severity = 17
	live.Body = "Live payment timeout recovered"
	mu.Lock()
	rows = append([]apigen.Log{live}, rows...)
	mu.Unlock()
	hub.Broadcast(liveevent.NewLogEvent(&liveevent.LogPayload{Row: &liveevent.SelfLogPayload{TimestampNs: live.TimestampNs, ReceivedAt: live.ReceivedAt, Severity: int(live.Severity), Body: live.Body, ServiceName: live.ServiceName, SessionID: live.SessionId, TraceID: live.TraceId, SpanID: live.SpanId, Attributes: live.Attributes}}))
	// Reconnect resync repairs a record missed by the shared socket.
	select {
	case socket := <-sockets:
		socket.Close()
	case <-ctx.Done():
		t.Fatal("socket did not connect")
	}
	gap := row
	gap.TimestampNs += 2000000000
	gap.ReceivedAt += 2000000000
	gap.Body = "Recovered missed log after reconnect"
	gap.Severity = 13
	mu.Lock()
	rows = append([]apigen.Log{gap}, rows...)
	mu.Unlock()
	send(tea.KeyShiftTab)
	wait("reconnecting")
	capture("logs-reconnecting")
	send(tea.KeyEsc)
	wait("Recovered missed log")
	wait("Live payment timeout")
	capture("logs-recovered")
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal(fmt.Errorf("terminal did not quit"))
	}
}
