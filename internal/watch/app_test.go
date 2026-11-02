package watch

import (
	"bytes"
	"context"
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/ws"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type terminalOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (o *terminalOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(p)
}
func (o *terminalOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.buffer.String() }

// This runs the production Bubble Tea program against seeded HTTP and real hub
// sockets, without a user console or a second collector/ingestion pipeline.
func TestSeededTerminalSmoke(t *testing.T) {
	if os.Getenv("SPANIEL_WATCH_CAPTURE_DIR") != "" {
		previousProfile, previousBackground := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile); lipgloss.SetHasDarkBackground(previousBackground) })
	}

	hub := ws.NewHub()
	requests := make(chan string, 128)
	sockets := make(chan net.Conn, 4)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			hub.ServeWS(w, r)
			return
		}
		select {
		case requests <- r.URL.Path:
		default:
		}
		if r.URL.Query().Get("sessionId") != "seed" {
			t.Error("unscoped terminal read")
		}
		var data any = []any{}
		switch r.URL.Path {
		case "/api/traces":
			data = []any{map[string]any{"trace_id": "seed-trace", "service_name": "api", "span_count": 1, "start_ns": int64(1793631000000000000)}}
		case "/api/spans", "/api/traces/seed-trace":
			data = []any{map[string]any{"trace_id": "seed-trace", "span_id": "seed-span", "name": "GET /seed", "service_name": "api"}}
		case "/api/metrics":
			data = []any{map[string]any{"name": "requests", "service_name": "api", "unit": "{request}"}}
		case "/api/logs":
			data = []any{map[string]any{"trace_id": "seed-trace", "span_id": "seed-span", "body": "seeded log"}}
		case "/api/lint":
			data = []any{map[string]any{"rule_id": "http.method", "message": "seeded lint"}}
		case "/api/dashboards":
			data = []any{map[string]any{"id": "seed-dashboard", "name": "Seed dashboard"}}
		case "/api/alerts":
			data = map[string]any{"items": []any{map[string]any{"id": "seed-alert", "name": "Seed alert"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]any{"total": 1}})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateHijacked {
			select {
			case sockets <- conn:
			default:
			}
		}
	}
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api, _ := client.New(srv.URL, client.Options{})
	model, err := New(ctx, api, Options{SessionID: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	var output terminalOutput
	p := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitPaths := func(paths ...string) {
		t.Helper()
		pending := map[string]bool{}
		for _, path := range paths {
			pending[path] = true
		}
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		for len(pending) > 0 {
			select {
			case path := <-requests:
				delete(pending, path)
			case <-timeout.C:
				t.Fatalf("terminal did not load %v", pending)
			}
		}
	}
	waitPaths("/api/traces", "/api/spans", "/api/metrics", "/api/logs", "/api/lint", "/api/issues", "/api/dashboards", "/api/alerts")
	capture := func(name string) {
		t.Helper()
		dir := os.Getenv("SPANIEL_WATCH_CAPTURE_DIR")
		if dir == "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Capture real renderer output before teardown restores the terminal.
	time.Sleep(100 * time.Millisecond)
	capture("watch-traces")

	p.Send(protocol.OpenTrace{TraceID: "seed-trace", SpanID: "seed-span"})
	waitPaths("/api/traces/seed-trace")
	p.Send(tea.KeyMsg{Type: tea.KeyTab})
	capture("watch-spans")
	hub.Broadcast(liveevent.NewSpanEvent(&liveevent.SpanPayload{TraceID: "seed-trace", SpanID: "late", SessionID: "seed"}))
	waitPaths("/api/traces", "/api/spans", "/api/traces/seed-trace")
	if os.Getenv("SPANIEL_WATCH_CAPTURE_DIR") != "" {
		select {
		case conn := <-sockets:
			conn.Close()
		case <-time.After(time.Second):
			t.Fatal("no live connection to interrupt")
		}
		deadline := time.Now().Add(time.Second)
		for !strings.Contains(output.text(), "reconnecting") {
			if time.Now().After(deadline) {
				t.Fatal("connection failure not visible")
			}
			time.Sleep(time.Millisecond)
		}
		capture("watch-reconnecting")
	}

	for i := 0; i < 6; i++ {
		p.Send(tea.KeyMsg{Type: tea.KeyTab})
	}
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal did not quit")
	}
	if !strings.Contains(output.text(), "spaniel - traces") || !strings.Contains(output.text(), "spaniel - spans") {
		t.Fatal("heading/window title missing from terminal output")
	}
	cancel()
}

func TestTerminalOutputSerializesStringAndByteWrites(t *testing.T) {
	var output terminalOutput
	if _, promoted := any(&output).(io.StringWriter); promoted {
		t.Fatal("unlocked StringWriter exposed")
	}
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		for range 1000 {
			io.WriteString(&output, "a")
		}
	}()
	go func() {
		defer writers.Done()
		for range 1000 {
			output.Write([]byte("b"))
			output.text()
		}
	}()
	writers.Wait()
	if len(output.text()) != 2000 {
		t.Fatal("concurrent terminal output lost bytes")
	}
}
