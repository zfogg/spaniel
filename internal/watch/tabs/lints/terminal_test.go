package lints

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/shell"
	"github.com/zfogg/spaniel/internal/ws"
)

type terminalOutput struct {
	sync.Mutex
	buffer bytes.Buffer
}

func (o *terminalOutput) Write(p []byte) (int, error) {
	o.Lock()
	defer o.Unlock()
	return o.buffer.Write(p)
}
func (o *terminalOutput) text() string { o.Lock(); defer o.Unlock(); return o.buffer.String() }

type otherTab struct{}

func (otherTab) Init() tea.Cmd                         { return nil }
func (o otherTab) Update(tea.Msg) (tea.Model, tea.Cmd) { return o, nil }
func (otherTab) View() string                          { return "Other tab for hidden-owner lifecycle verification" }

// Run the actual shell and Bubble Tea renderer against HTTP-shaped snapshots
// and the real shared WebSocket hub. Captures contain no recreated UI.
func TestRunningFindingsHiddenRefreshAndCapture(t *testing.T) {
	if os.Getenv("SPANIEL_WATCH_CAPTURE_DIR") != "" {
		profile, bg := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(bg) })
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lints, issues, spans := fixtures()
	var mu sync.Mutex
	hub := ws.NewHub()
	requests := make(chan string, 128)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			hub.ServeWS(w, r)
			return
		}
		if r.URL.Query().Get("sessionId") != "demo" {
			t.Error("unscoped request")
		}
		var data any
		mu.Lock()
		switch r.URL.Path {
		case "/api/lint":
			data = append([]apigen.LintWarning(nil), lints...)
		case "/api/issues":
			data = append([]apigen.TraceIssue(nil), issues...)
		case "/api/spans":
			if r.URL.Query().Get("limit") != "1000" || r.URL.Query().Get("view") != "flat" {
				t.Error("unbounded service lookup")
			}
			data = spans
		case "/api/traces/checkout-trace":
			data = []apigen.Span{{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span", ServiceName: "checkout-api"}, {SessionId: "demo", TraceId: "checkout-trace", SpanId: "db-span", ServiceName: "catalog-db"}}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
		select {
		case requests <- r.URL.Path:
		default:
		}
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
	api, err := client.New(srv.URL, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	updates, err := api.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refresh := client.NewRefreshes(ctx, api)
	refresh.SetSession("demo")
	env := protocol.Env{Context: ctx, API: api, Refresh: refresh}
	var tabs []protocol.Tab
	for _, id := range protocol.Order {
		factory := func(protocol.Env) tea.Model { return otherTab{} }
		if id == protocol.Lints {
			factory = New
		}
		tabs = append(tabs, protocol.Tab{ID: id, Title: string(id), New: factory})
	}
	app := shell.New(env, tabs, nil, updates, true)
	output := &terminalOutput{}
	p := tea.NewProgram(app, tea.WithInput(nil), tea.WithOutput(output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	for i := 0; i < 4; i++ {
		p.Send(tea.KeyMsg{Type: tea.KeyTab})
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				t.Fatalf("terminal state timed out: %s", output.text())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitText := func(text string) {
		t.Helper()
		wait(func() bool { return strings.Contains(ansi.Strip(output.text()), text) })
	}
	capture := func(name string) {
		t.Helper()
		dir := os.Getenv("SPANIEL_WATCH_CAPTURE_DIR")
		if dir == "" {
			return
		}
		time.Sleep(120 * time.Millisecond)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	waitText("2 semantic")
	waitText("catalog-db")
	p.Send(tea.KeyMsg{Type: tea.KeyHome})
	capture("watch-lints")
	// Open an actual semantic detail, retain it while switching tabs, and update
	// durable lint data without using the old issue event.
	p.Send(key("enter"))
	waitText("Finding · http.request.method")
	waitText("t open trace")
	capture("watch-lint-detail")
	p.Send(tea.KeyMsg{Type: tea.KeyTab})
	waitText("Other tab")
	for len(requests) > 0 {
		<-requests
	}
	mu.Lock()
	lints[0].Message = "Late lint repair received while hidden"
	issues[0].Count = 37
	mu.Unlock()
	hub.Broadcast(liveevent.NewResourcesChangedEvent([]liveevent.Resource{"lint", "issues"}, "analysis", "demo"))
	// Both the hidden list and its retained detail must own a repair request.
	repairs := 0
	deadline := time.After(8 * time.Second)
	for repairs < 2 {
		select {
		case path := <-requests:
			if path == "/api/lint" {
				repairs++
			}
		case <-deadline:
			t.Fatal("hidden finding owners did not refresh")
		}
	}
	p.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	waitText("Late lint repair received while hidden")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitText("Findings")
	p.Send(key("/"))
	p.Send(key("rule:n_plus_one"))
	p.Send(key("enter"))
	waitText("1 matching")
	p.Send(key("enter"))
	waitText("Fingerprint: SELECT")
	waitText("Count: 37")
	capture("watch-detector-detail")
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not shut down")
	}
}
