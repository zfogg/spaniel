package dashboards_test

import (
	"bytes"
	"context"
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
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/ws"
)

type terminalOutput struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (o *terminalOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.data.Write(p)
}
func (o *terminalOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.data.String() }

// The real app exercises the owner lifecycle, shared live stream, generated
// client contracts, safe query API and ntcharts renderer against stored spans.
func TestDashboardTerminalRealAPI(t *testing.T) {
	if os.Getenv("SPANIEL_DASHBOARD_CAPTURE_DIR") != "" {
		profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	}
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.CreateSession("dashboard review", false)
	if err != nil {
		t.Fatal(err)
	}
	store.SetActiveSession(session.ID, session.Label)
	now := time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC)
	insert := func(i int) {
		t.Helper()
		start := now.Add(time.Duration(i) * time.Minute).UnixNano()
		if err := store.InsertSpan(&storage.Span{TraceID: fmt.Sprintf("checkout-%d", i), SpanID: fmt.Sprintf("span-%d", i), Name: "POST /checkout", ServiceName: "checkout-api", SessionID: session.ID, SessionLabel: session.Label, StartNs: start, EndNs: start + int64((20+i)*1000000), Attributes: "{}", Resource: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		insert(i)
	}
	dashboard := &storage.Dashboard{ID: "checkout-review", Name: "Checkout overview", Description: "Stored checkout telemetry · read-only terminal dashboard", SourceFile: "dashboards/checkout.yaml", SourceHash: "review-v1", SourceFileModified: true, Variables: []*storage.DashboardVariable{{Name: "service", Kind: "enum", OptionsJSON: `["checkout-api","worker"]`, DefaultValue: "checkout-api", Source: "telemetry_spans.service_name"}}, Panels: []*storage.DashboardPanel{
		{Title: "Latency by minute", Position: 0, DisplayType: "time_series", QuerySQL: "SELECT (start_ns // 60000000000) * 60000000000 AS timestamp_ns, avg(duration_ns) / 1000000 AS value, service_name AS group_value FROM telemetry_spans WHERE session_id = $session_id AND service_name = $service GROUP BY 1, 3 ORDER BY 1", SettingsJSON: `{"unit":"ms"}`},
		{Title: "Request count", Position: 1, DisplayType: "single_value", QuerySQL: "SELECT count(*) AS value FROM telemetry_spans WHERE session_id = $session_id AND service_name = $service", SettingsJSON: `{"unit":"requests"}`},
		{Title: "Stored requests", Position: 2, DisplayType: "table", QuerySQL: "SELECT name, service_name, duration_ns, NULL AS optional FROM telemetry_spans WHERE session_id = $session_id AND service_name = $service ORDER BY start_ns DESC LIMIT 3", SettingsJSON: "{}"},
		{Title: "Category fallback", Position: 3, DisplayType: "bar", QuerySQL: "SELECT service_name AS label, count(*) AS value FROM telemetry_spans GROUP BY 1", SettingsJSON: "{}"},
		{Title: "Invalid saved query", Position: 4, DisplayType: "single_value", QuerySQL: "SELECT value FROM missing_review_table", SettingsJSON: "{}"},
	}}
	if err := store.ReplaceDashboardDefinition(dashboard); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	store.SetChangeNotifier(func(names []string, reason string) {
		resources := []liveevent.Resource{}
		for _, name := range names {
			resources = append(resources, liveevent.Resource(name))
		}
		hub.Broadcast(liveevent.NewResourcesChangedEvent(resources, reason, ""))
	})
	router := api.NewRouter(store, hub, nil)
	sockets := make(chan net.Conn, 8)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && (r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/query-preview")) {
			t.Errorf("UI mutation: %s %s", r.Method, r.URL.Path)
		}
		if r.Method == "GET" && r.URL.Path == "/api/dashboards/checkout-review" {
			t.Error("detail GET acknowledges definition")
		}
		router.ServeHTTP(w, r)
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
	apiClient, err := client.New(srv.URL, client.Options{MinBackoff: time.Second, MaxBackoff: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	model, err := watch.New(ctx, apiClient, watch.Options{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	var output terminalOutput
	p := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer func() {
		cancel()
		p.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("terminal shutdown timed out")
		}
	}()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 38})
	for i := 0; i < 5; i++ {
		p.Send(tea.KeyMsg{Type: tea.KeyTab})
	}
	waitFrom := func(needle string, offset int) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for !strings.Contains(output.text()[offset:], needle) {
			if time.Now().After(deadline) {
				t.Fatalf("terminal missing %q: %s", needle, output.text())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait := func(needle string) { t.Helper(); waitFrom(needle, 0) }
	capture := func(name string) {
		t.Helper()
		dir := os.Getenv("SPANIEL_DASHBOARD_CAPTURE_DIR")
		if dir == "" {
			return
		}
		time.Sleep(150 * time.Millisecond)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	send := func(k string) { p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}) }
	wait("checkout.yaml")
	capture("dashboard-list")
	p.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("Series: checkout-api")
	capture("dashboard-series")
	send("i")
	wait("Query result")
	capture("dashboard-inspection")
	send("i")
	send("e")
	wait("Set $service")
	capture("dashboard-variable")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	send("l")
	wait("12 requests")
	capture("dashboard-stat")
	// Leave the retained detail hidden, add durable data and return to its result.
	p.Send(tea.KeyMsg{Type: tea.KeyTab})
	insert(12)
	hub.Broadcast(liveevent.NewResourcesChangedEvent([]liveevent.Resource{"dashboard-panel"}, "seeded late data", session.ID))
	time.Sleep(150 * time.Millisecond)
	p.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	wait("13 requests")
	send("l")
	wait("NULL")
	capture("dashboard-table")
	send("l")
	wait("unsupported in the terminal")
	capture("dashboard-fallback")
	send("l")
	wait("missing_review_table")
	wait("Query error:")
	capture("dashboard-error")
	// Disconnect the one socket; the app retains data and authoritatively resyncs.
	select {
	case conn := <-sockets:
		conn.Close()
	case <-time.After(time.Second):
		t.Fatal("no live socket")
	}
	wait("reconnecting")
	capture("dashboard-reconnecting")
	select {
	case <-sockets:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not reconnect")
	}
	// External metadata update and removal use durable storage notifications.
	dashboard.Name = "Checkout revised externally"
	dashboard.Panels, dashboard.Variables = nil, nil
	if err := store.UpdateDashboard(dashboard); err != nil {
		t.Fatal(err)
	}
	wait("Checkout revised externally")
	if updated, err := store.DefinitionUpdated("dashboard", dashboard.ID, dashboard.SourceHash, dashboard.UpdatedAt); err != nil || !updated {
		t.Fatal("read-only UI acknowledged definition", updated, err)
	}
	// Record the physical resize boundary for faithful xterm replay. Replaying
	// all prior 120-column frames at the narrow size would fabricate a screen.
	selectionOffset := len(output.text())
	send("h")
	send("h")
	waitFrom("NULL", selectionOffset)
	if dir := os.Getenv("SPANIEL_DASHBOARD_CAPTURE_DIR"); dir != "" {
		metadata := fmt.Sprintf(`{"offset":%d,"cols":60,"rows":28}`, len(output.text()))
		if err := os.WriteFile(filepath.Join(dir, "dashboard-narrow.resize.json"), []byte(metadata), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p.Send(tea.WindowSizeMsg{Width: 60, Height: 28})
	capture("dashboard-narrow")
	if err := store.DeleteDashboard(dashboard.ID); err != nil {
		t.Fatal(err)
	}
	wait("Dashboard removed externally")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	wait("No saved or file-managed dashboards")
}
