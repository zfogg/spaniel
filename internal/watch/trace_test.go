package watch

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestStandaloneTraceUsesLiveSharedWaterfallInspectorAndFooter(t *testing.T) {
	hub := ws.NewHub()
	var sockets atomic.Int32
	span := map[string]any{"session_id": "pinned", "trace_id": "trace", "span_id": "span", "service_name": "api", "name": "GET /standalone", "attributes": `{"exact":9007199254740993}`, "start_ns": int64(1793631000000000001), "end_ns": int64(1793631000240000001), "duration_ns": int64(240000000)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			sockets.Add(1)
			hub.ServeWS(w, r)
			return
		}
		if r.URL.Query().Get("sessionId") != "pinned" {
			t.Error("standalone scope lost", r.URL)
		}
		var data any = []any{}
		switch r.URL.Path {
		case "/api/traces/trace":
			data = []any{span}
		case "/api/spans/span":
			data = span
		case "/api/traces":
			data = []any{map[string]any{"trace_id": "trace", "session_id": "pinned", "name": "GET /standalone"}}
		case "/api/logs":
		default:
			t.Error("standalone loaded unrelated tab", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api, _ := client.New(srv.URL, client.Options{})
	model, err := newTraceApp(ctx, api, "trace", "pinned")
	if err != nil {
		t.Fatal(err)
	}
	var output terminalOutput
	program := tea.NewProgram(inspectedApp{model}, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer func() {
		program.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			program.Kill()
			t.Error("standalone did not stop")
		}
	}()
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	wait := func(text string) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			reply := make(chan string, 1)
			program.Send(inspectApp{reply})
			select {
			case view := <-reply:
				plain := ansi.Strip(view)
				if strings.Contains(plain, text) {
					return plain
				}
				if time.Now().After(deadline) {
					t.Fatalf("missing %q:\n%s", text, plain)
				}
			case <-time.After(time.Second):
				t.Fatal("standalone stopped responding")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	view := wait("inspect span")
	if !strings.Contains(view, "Waterfall") || strings.Contains(strings.Split(view, "\n")[0], "metrics") || sockets.Load() != 1 {
		t.Fatal("standalone shell composition", view, sockets.Load())
	}
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	view = wait("9007199254740993")
	if !strings.Contains(view, "Attributes · key/value table") || !strings.Contains(view, "t containing trace") {
		t.Fatal("obsolete renderer used", view)
	}
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	wait("Keyboard help")
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	wait("Waterfall")
}
