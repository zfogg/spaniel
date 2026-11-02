package traces_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
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
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/ws"
)

type inspectScreen struct{ reply chan string }
type inspectedModel struct{ model tea.Model }

func (m inspectedModel) Init() tea.Cmd { return m.model.Init() }
func (m inspectedModel) View() string  { return m.model.View() }
func (m inspectedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if inspect, ok := msg.(inspectScreen); ok {
		inspect.reply <- m.model.View()
		return m, nil
	}
	next, cmd := m.model.Update(msg)
	m.model = next
	return m, cmd
}

type recording struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (r *recording) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.Write(p)
}
func (r *recording) text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.data.String() }

// Real storage -> canonical HTTP -> shared socket/refresh scheduler -> production
// shell and feature constructors -> running Bubble Tea renderer. Captures contain
// only safe seeded telemetry, never a reconstruction of the feature's View.
func TestSeededTraceSpanTerminal(t *testing.T) {
	if os.Getenv("SPANIEL_TRACE_CAPTURE_DIR") != "" {
		old, bg := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(old); lipgloss.SetHasDarkBackground(bg) })
	}
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.CreateSession("watch trace demo", false)
	if err != nil {
		t.Fatal(err)
	}
	store.SetActiveSession(session.ID, session.Label)
	const traceID = "8e513fc410244cac9f6af286f8d9b125"
	const rootID = "a000000000000001"
	const dbID = "b000000000000002"
	const start int64 = 1793631000000000000
	seed := func(id, parent, service, name string, offset, dur int64, status int) *storage.Span {
		s := &storage.Span{SessionID: session.ID, SessionLabel: session.Label, TraceID: traceID, SpanID: id, ParentSpanID: parent, ServiceName: service, Name: name, Kind: 3, StartNs: start + offset, EndNs: start + offset + dur, DurationNs: dur, StatusCode: status, Attributes: `{"db.system.name":"postgresql","db.query.text":"SELECT id FROM orders WHERE customer_id = $1","db.response.returned_rows":9007199254740993,"retryable":true,"retry":{"attempt":2,"delays_ms":[25,50]}}`, Resource: `{"service.version":"1.4.2","deployment.environment.name":"demo","deployment.revision":18446744073709551615}`, ReceivedAt: start + offset}
		if status == 2 {
			s.StatusMessage = "query deadline exceeded"
		}
		if err := store.InsertSpan(s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	seed(rootID, "", "checkout-api", "GET /orders", 0, 240_000_000, 2)
	seed(dbID, rootID, "orders-db", "SELECT orders", 20_000_000, 170_000_000, 2)
	seed("c000000000000003", rootID, "cache", "GET customer", 5_000_000, 10_000_000, 1)
	if err := store.InsertSpanEvents([]*storage.SpanEvent{{SessionID: session.ID, TraceID: traceID, SpanID: dbID, Name: "exception", TimeNs: start + 180_000_000, Attributes: `{"exception.type":"TimeoutError","exception.message":"query deadline exceeded"}`}}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSpanLinks([]*storage.SpanLink{{SessionID: session.ID, TraceID: traceID, SpanID: dbID, LinkedTraceID: "a9b72c0508c64a209537cae5c8136d37", LinkedSpanID: "d000000000000004", TraceState: "demo=sampled", Attributes: `{"link.reason":"retry"}`}}); err != nil {
		t.Fatal(err)
	}
	log := &storage.Log{SessionID: session.ID, TraceID: traceID, SpanID: dbID, ServiceName: "orders-db", TimestampNs: start + 185_000_000, ReceivedAt: start + 185_000_000, Severity: 17, Body: "query exceeded the 150ms budget", Attributes: `{"retryable":true}`}
	if err := store.InsertLog(log); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(api.NewRouter(store, hub, nil))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiClient, err := client.New(srv.URL, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	model, err := watch.New(ctx, apiClient, watch.Options{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	var output recording
	p := tea.NewProgram(inspectedModel{model}, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer func() {
		p.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			p.Kill()
			t.Error("terminal did not stop")
		}
	}()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	snapshot := func() string {
		ch := make(chan string, 1)
		p.Send(inspectScreen{reply: ch})
		select {
		case view := <-ch:
			return ansi.Strip(view)
		case <-time.After(3 * time.Second):
			t.Fatal("UI loop stopped")
			return ""
		}
	}
	wait := func(text string) string {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for {
			view := snapshot()
			if strings.Contains(view, text) {
				return view
			}
			if time.Now().After(deadline) {
				t.Fatalf("missing %q in running UI:\n%s", text, view)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	capture := func(name string) {
		t.Helper()
		dir := os.Getenv("SPANIEL_TRACE_CAPTURE_DIR")
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
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	wait("GET /orders (3 spans)")
	capture("traces-list")
	p.Send(enter)
	wait("Waterfall")
	capture("trace-waterfall")
	late := seed("e000000000000005", rootID, "inventory-db", "SELECT inventory (late)", 195_000_000, 30_000_000, 1)
	rowData, _ := json.Marshal(late)
	var row liveevent.SelfSpanPayload
	if err := json.Unmarshal(rowData, &row); err != nil {
		t.Fatal(err)
	}
	hub.Broadcast(liveevent.NewSpanEvent(&liveevent.SpanPayload{TraceID: traceID, SpanID: late.SpanID, SessionID: session.ID, Row: &row}))
	wait("SELECT inventory (late)")
	capture("trace-late-span")
	// Cache child starts first; the next down selects the error database span.
	p.Send(tea.KeyMsg{Type: tea.KeyDown})
	p.Send(tea.KeyMsg{Type: tea.KeyDown})
	p.Send(enter)
	wait("query deadline exceeded")
	capture("span-inspector")
	p.Send(tea.KeyMsg{Type: tea.KeyPgDown})
	wait("Resource · key/value table")
	capture("span-resource-table")
	p.Send(tea.KeyMsg{Type: tea.KeyEnd})
	wait("query exceeded the 150ms budget")
	capture("span-logs")
	log.Body = "late log: cancelled database request"
	log.ReceivedAt++
	log.TimestampNs++
	if err := store.InsertLog(log); err != nil {
		t.Fatal(err)
	}
	logData, _ := json.Marshal(log)
	var logRow liveevent.SelfLogPayload
	json.Unmarshal(logData, &logRow)
	hub.Broadcast(liveevent.NewLogEvent(&liveevent.LogPayload{TraceID: traceID, SpanID: dbID, SessionID: session.ID, Row: &logRow}))
	p.Send(tea.KeyMsg{Type: tea.KeyEnd})
	wait("late log: cancelled database request")
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	view := wait("Waterfall")
	if !strings.Contains(view, "›   SELECT orders") {
		t.Fatalf("containing trace did not select origin span:\n%s", view)
	}
	p.Send(esc)
	wait("Span " + dbID)
	p.Send(esc)
	wait("Waterfall")
	p.Send(tea.KeyMsg{Type: tea.KeyTab})
	wait("individual spans")
	capture("spans-list")
	p.Send(enter)
	wait("query deadline exceeded")
	capture("spans-tab-inspector")
	p.Send(tea.KeyMsg{Type: tea.KeyPgDown})
	wait("Resource · key/value table")
	capture("spans-tab-resource-table")
	p.Send(esc)
	wait("individual spans")
	// The retained waterfall keeps receiving its owner results and live rows.
	hidden := seed("f000000000000006", rootID, "worker", "hidden detail update", 230_000_000, 1_000_000, 1)
	rowData, _ = json.Marshal(hidden)
	json.Unmarshal(rowData, &row)
	hub.Broadcast(liveevent.NewSpanEvent(&liveevent.SpanPayload{TraceID: traceID, SpanID: hidden.SpanID, SessionID: session.ID, Row: &row}))
	wait("hidden detail update")
	p.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	wait("hidden detail update")
	// An external typed link reaches exactly the same inspector and Back view.
	p.Send(protocol.OpenSpan{TraceID: traceID, SpanID: dbID, Return: protocol.ReturnContext{Tab: protocol.Traces, Selection: session.ID + "/" + dbID}})
	wait("query deadline exceeded")
	p.Send(esc)
	wait("Waterfall")
}
