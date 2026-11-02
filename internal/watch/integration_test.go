package watch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type inspectApp struct{ reply chan string }
type inspectedApp struct{ tea.Model }

func (m inspectedApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if request, ok := msg.(inspectApp); ok {
		request.reply <- m.Model.View()
		return m, nil
	}
	next, cmd := m.Model.Update(msg)
	m.Model = next
	return m, cmd
}

// One real storage/API/hub instance exercises all production feature factories.
// External fixture writes represent collectors/evaluators/definition sync; the
// terminal is observed to issue GETs and read-only query previews exclusively.
func TestAllSevenRealAPINavigationRepairAndContextualFooter(t *testing.T) {
	captureDir := os.Getenv("SPANIEL_INTEGRATION_CAPTURE_DIR")
	if captureDir != "" {
		profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		defer func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) }()
	}
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	selected, err := db.CreateSession("Integration checkout", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(selected.ID, selected.Label)
	hub := ws.NewHub()
	db.SetChangeNotifier(func(names []string, reason string) {
		resources := make([]liveevent.Resource, len(names))
		for i, name := range names {
			resources[i] = liveevent.Resource(name)
		}
		hub.Broadcast(liveevent.NewResourcesChangedEvent(resources, reason, ""))
	})
	pipeline := ingestion.NewPipeline(db, hub)
	defer pipeline.Close(context.Background())
	const traceID = "8e513fc410244cac9f6af286f8d9b125"
	const rootID = "a000000000000001"
	const childID = "b000000000000002"
	now := time.Now().Add(-time.Minute).Truncate(time.Second)
	insertSpan := func(id, parent, name string, offset int64) {
		t.Helper()
		start := now.UnixNano() + offset
		if err := db.InsertSpan(&storage.Span{SessionID: selected.ID, SessionLabel: selected.Label, TraceID: traceID, SpanID: id, ParentSpanID: parent, ServiceName: "checkout-api", Name: name, Kind: 3, StatusCode: 2, StatusMessage: "query deadline exceeded", StartNs: start, EndNs: start + 240000000, DurationNs: 240000000, Attributes: `{"order.id":9007199254740993,"retry":{"delays_ms":[25,50],"enabled":true}}`, Resource: `{"service.version":"1.4.2","deployment.environment.name":"demo"}`, ReceivedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	insertSpan(rootID, "", "GET /orders", 0)
	insertSpan(childID, rootID, "SELECT orders", 10000000)
	if err := db.InsertSpanEvents([]*storage.SpanEvent{{SessionID: selected.ID, TraceID: traceID, SpanID: childID, TimeNs: now.UnixNano() + 20000000, Name: "exception", Attributes: `{"exception.type":"DeadlineExceeded"}`}}); err != nil {
		t.Fatal(err)
	}
	insertLog := func(body string, offset int64) {
		t.Helper()
		if err := db.InsertLog(&storage.Log{SessionID: selected.ID, TraceID: traceID, SpanID: childID, ServiceName: "checkout-api", TimestampNs: now.UnixNano() + offset, ReceivedAt: now.UnixNano() + offset, Severity: 17, Body: body, Attributes: `{"order.id":9007199254740993,"attempts":[1,2],"retryable":true}`}); err != nil {
			t.Fatal(err)
		}
	}
	insertLog("Order lookup deadline exceeded", 20000000)
	if err := db.InsertLintWarning(&storage.LintWarning{SessionID: selected.ID, TraceID: traceID, SpanID: childID, RuleID: "db.query.text", Message: "Review parameterized database query", Severity: "warning", CreatedAt: now.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	metrics := func(value int64) pmetric.Metrics {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("service.name", "checkout-api")
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("integration")
		sm.Scope().Attributes().PutInt("build", 9007199254740993)
		metric := sm.Metrics().AppendEmpty()
		metric.SetName("checkout.queue.depth")
		metric.SetDescription("Queued orders awaiting processing")
		metric.SetUnit("{order}")
		gauge := metric.SetEmptyGauge()
		for i := 0; i < 8; i++ {
			point := gauge.DataPoints().AppendEmpty()
			point.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Duration(i+int(value)-10) * time.Second)))
			point.SetIntValue(value + int64(i))
			point.Attributes().PutStr("queue", "orders")
			point.Attributes().PutInt("build", 9007199254740993)
		}
		return md
	}
	if err := pipeline.IngestMetrics(context.Background(), metrics(10)); err != nil {
		t.Fatal(err)
	}
	dashboard := &storage.Dashboard{ID: "checkout", Name: "Checkout overview", Description: "Pinned recording · read-only", Variables: []*storage.DashboardVariable{{Name: "service", Kind: "enum", DefaultValue: "checkout-api", OptionsJSON: `["checkout-api","worker"]`}}, Panels: []*storage.DashboardPanel{
		{Title: "Latency by second", Position: 0, DisplayType: "time_series", QuerySQL: "SELECT start_ns AS timestamp_ns, duration_ns/1000000.0 AS value FROM telemetry_spans WHERE session_id=$session_id AND service_name=$service ORDER BY start_ns", SettingsJSON: `{"unit":"ms"}`},
		{Title: "Exact SQL inspection", Position: 1, DisplayType: "table", QuerySQL: "SELECT 9007199254740993::BIGINT AS exact, count(*) AS spans FROM telemetry_spans WHERE session_id=$session_id", SettingsJSON: "{}"},
	}}
	if err := db.ReplaceDashboardDefinition(dashboard); err != nil {
		t.Fatal(err)
	}
	rule := &storage.AlertRule{ID: "latency", Name: "Checkout latency", Enabled: true, Severity: "critical", QuerySQL: "SELECT max(duration_ns)/1e6 AS value FROM spans", ConditionJSON: `{"kind":"threshold","operator":">","value":200}`, GroupByJSON: `["service.name"]`, AnnotationsJSON: fmt.Sprintf(`{"trace_id":"%s","span_id":"%s","runbook":"https://example.com/runbooks/orders"}`, traceID, childID), SourceFile: "alerts/demo.yaml", SourceHash: "version-one"}
	if err := db.CreateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	value := 240.0
	if err := db.UpsertAlertInstance(&storage.AlertInstance{RuleID: rule.ID, GroupKey: "checkout", State: "firing", Value: &value, LastEvaluatedAt: now.UnixNano(), LabelsJSON: fmt.Sprintf(`{"service.name":"checkout-api","trace_id":"%s","span_id":"%s","build":9007199254740993}`, traceID, childID)}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAlertEvent(&storage.AlertEvent{ID: "firing-event", RuleID: rule.ID, GroupKey: "checkout", Kind: "firing", State: "firing", Value: &value, Detail: "Order lookup exceeded 200ms", CreatedAt: now.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	// Pin selected while another server-active session contains competing data.
	other, err := db.CreateSession("Other active recording", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(other.ID, other.Label)
	if err := db.InsertSpan(&storage.Span{SessionID: other.ID, TraceID: "foreign-trace", SpanID: "foreign-span", Name: "FOREIGN telemetry", ServiceName: "other", StartNs: now.UnixNano(), Attributes: "{}", Resource: "{}"}); err != nil {
		t.Fatal(err)
	}
	router := api.NewRouter(db, hub, nil)
	var offline atomic.Bool
	var sockets atomic.Int32
	connections := make(chan net.Conn, 8)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" && offline.Load() {
			http.Error(w, "forced offline", 503)
			return
		}
		if r.Method != "GET" && (r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/query-preview")) {
			t.Errorf("terminal mutation: %s %s", r.Method, r.URL)
		}
		if r.URL.Path == "/api/alerts/latency" && r.URL.Query().Get("markSeen") != "false" {
			t.Error("alert inspection would mark definition seen")
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
			sockets.Add(1)
			connections <- conn
		}
	}
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiClient, err := client.New(srv.URL, client.Options{MinBackoff: 100 * time.Millisecond, MaxBackoff: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(ctx, apiClient, Options{SessionID: selected.ID})
	if err != nil {
		t.Fatal(err)
	}
	var output terminalOutput
	program := tea.NewProgram(inspectedApp{app}, tea.WithAltScreen(), tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
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
			t.Error("UI did not stop")
		}
	}()
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	snapshot := func() string {
		t.Helper()
		reply := make(chan string, 1)
		program.Send(inspectApp{reply})
		select {
		case view := <-reply:
			return ansi.Strip(view)
		case <-time.After(3 * time.Second):
			t.Fatal("UI stopped responding")
			return ""
		}
	}
	wait := func(text string) string {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			view := snapshot()
			if strings.Contains(view, text) {
				if strings.Contains(view, "FOREIGN telemetry") {
					t.Fatal("pinned scope leaked", view)
				}
				return view
			}
			if time.Now().After(deadline) {
				t.Fatalf("missing %q:\n%s", text, view)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	send := func(s string) {
		switch s {
		case "tab":
			program.Send(tea.KeyMsg{Type: tea.KeyTab})
		case "back":
			program.Send(tea.KeyMsg{Type: tea.KeyEsc})
		case "enter":
			program.Send(tea.KeyMsg{Type: tea.KeyEnter})
		default:
			program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		}
	}
	capture := func(name string) {
		t.Helper()
		if captureDir == "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
		if err := os.MkdirAll(captureDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(captureDir, "integration-"+name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	footer := func(view, action string) {
		t.Helper()
		rows := strings.Split(view, "\n")
		bottom := strings.Join(rows[max(0, len(rows)-6):], "\n")
		if !strings.Contains(bottom, action) {
			t.Fatalf("focused footer missing %q:\n%s", action, view)
		}
	}
	// Bootstrap every feature while all but one are hidden.
	for _, step := range []struct{ tab, text, action string }{{"traces", "GET /orders", "open"}, {"spans", "SELECT orders", "open"}, {"metrics", "checkout.queue.depth", "inspect series"}, {"logs", "Order lookup deadline exceeded", "full log"}, {"lints", "Review parameterized database query", "inspect finding"}, {"dashboards", "Checkout overview", "open panels"}, {"alerts", "Checkout latency", "open rule"}} {
		view := wait(step.text)
		if !strings.HasPrefix(view, "spaniel - "+step.tab) {
			t.Fatal("wrong active heading", view)
		}
		footer(view, step.action)
		capture(step.tab)
		send("tab")
	}
	if sockets.Load() != 1 {
		t.Fatalf("one app opened %d sockets", sockets.Load())
	}
	// Traces -> waterfall -> span. The same inspector is reached from spans.
	wait("GET /orders")
	send("enter")
	footer(wait("Waterfall"), "inspect span")
	capture("waterfall")
	send("enter")
	footer(wait("Attributes · key/value table"), "containing trace")
	capture("trace-span")
	send("back")
	send("back")
	send("tab")
	wait("SELECT orders")
	send("enter")
	view := wait("9007199254740993")
	footer(view, "containing trace")
	capture("spans-inspector")
	// Metadata stays a compact table when a physical resize clips long IDs.
	if captureDir != "" {
		data, _ := json.Marshal(map[string]int{"offset": len(output.text()), "cols": 40, "rows": 30})
		if err := os.WriteFile(filepath.Join(captureDir, "integration-span-metadata-narrow.resize.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	program.Send(tea.WindowSizeMsg{Width: 40, Height: 30})
	program.Send(tea.ClearScreen())
	narrow := snapshot()
	if !strings.Contains(narrow, "Trace ID  │ "+traceID[:27]+"…") {
		t.Fatal("long metadata did not use a one-row ellipsis", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatal("narrow metadata overflowed", narrow)
		}
	}
	capture("span-metadata-narrow")
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	program.Send(tea.ClearScreen())
	send("t")
	wait("Waterfall")
	send("back")
	wait("9007199254740993")
	send("back")
	send("tab")
	// Metric admission is independent of chart-window samples and exact labels.
	wait("checkout.queue.depth")
	send("enter")
	wait("1 active series / 2000 limit")
	capture("metric-series")
	send("/")
	footer(wait("apply label filter"), "cancel label filter")
	capture("metric-input")
	send("back")
	send("tab")
	// Logs and lint correlation navigate through the shared selected-span viewer.
	wait("Order lookup deadline exceeded")
	send("enter")
	footer(wait("Full log"), "correlated span")
	capture("log-detail")
	send("s")
	wait("9007199254740993")
	send("back")
	wait("Full log")
	send("back")
	send("tab")
	wait("Review parameterized database query")
	send("enter")
	footer(wait("open span"), "open trace")
	capture("lint-detail")
	send("s")
	wait("9007199254740993")
	send("back")
	wait("Finding ·")
	send("back")
	send("tab")
	// Pinned previews work despite the different active recording. User SQL and
	// exact dynamic result numbers remain intact in secondary inspection.
	wait("Checkout overview")
	send("enter")
	wait("Latency by second")
	capture("dashboard-chart")
	send("e")
	footer(wait("Set $service"), "apply value")
	capture("dashboard-input")
	send("back")
	send("l")
	send("i")
	wait("9007199254740993")
	capture("dashboard-sql")
	send("back")
	send("tab")
	wait("Checkout latency")
	send("enter")
	wait("Condition / threshold")
	capture("alert-definition")
	send("i")
	wait("240")
	capture("alert-instances")
	send("enter")
	wait("State: firing")
	capture("alert-instance")
	send("back")
	send("h")
	wait("Order lookup exceeded 200ms")
	capture("alert-history")
	send("l")
	wait(childID)
	capture("alert-links")
	send("enter")
	wait("9007199254740993")
	send("back")
	wait("Links from rule")
	db.SetActiveSession(selected.ID, selected.Label)
	hub.Broadcast(liveevent.NewActiveSessionEvent(&liveevent.ActiveSessionPayload{ID: selected.ID, Label: selected.Label}))
	if err := pipeline.IngestMetrics(context.Background(), metrics(30)); err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(other.ID, other.Label)
	hub.Broadcast(liveevent.NewActiveSessionEvent(&liveevent.ActiveSessionPayload{ID: other.ID, Label: other.Label}))
	if err := db.InsertLintWarning(&storage.LintWarning{SessionID: selected.ID, TraceID: traceID, SpanID: childID, RuleID: "db.response.returned_rows", Message: "Late semantic lint", Severity: "warning", CreatedAt: now.Add(time.Second).UnixNano()}); err != nil {
		t.Fatal(err)
	}
	rule.Name = "Checkout latency revised"
	if err := db.UpdateAlertRule(rule); err != nil {
		t.Fatal(err)
	}
	wait("Checkout latency revised")
	if updated, err := db.DefinitionUpdated("alert", rule.ID, rule.SourceHash, rule.UpdatedAt); err != nil || !updated {
		t.Fatal("read-only terminal acknowledged alert", updated, err)
	}
	// Reconnect repairs an event deliberately missed while offline. Retained
	// alert details keep their owner/result context across the interruption.
	offline.Store(true)
	select {
	case conn := <-connections:
		conn.Close()
	case <-time.After(time.Second):
		t.Fatal("no socket to interrupt")
	}
	wait("reconnecting")
	capture("disconnected")
	insertSpan("c000000000000003", rootID, "Missed inventory lookup", 30000000)
	insertLog("Missed log after reconnect", 40000000)
	offline.Store(false)
	wait("● live")
	send("back")
	send("tab")
	wait("3 spans")
	capture("recovered-traces")
	// External definitions repair hidden owners, and removals clear retained
	// details. No terminal mutation is used to arrange any of these changes.
	dashboard.Name = "Checkout renamed externally"
	if err := db.ReplaceDashboardDefinition(dashboard); err != nil {
		t.Fatal(err)
	}
	send("tab")
	wait("Missed inventory lookup")
	send("tab")
	wait("1 active series / 2000 limit")
	wait("37 {order}")
	capture("metric-hidden-repair")
	send("back")
	send("tab")
	wait("Missed log after reconnect")
	send("tab")
	wait("Late semantic lint")
	capture("lint-hidden-repair")
	send("tab")
	wait("Checkout renamed externally")
	send("enter")
	wait("Latency by second")
	if err := db.DeleteDashboard(dashboard.ID); err != nil {
		t.Fatal(err)
	}
	wait("Dashboard removed externally")
	capture("dashboard-removed")
	send("back")
	send("tab")
	if err := db.DeleteAlertRule(rule.ID); err != nil {
		t.Fatal(err)
	}
	wait("No alert rules")
	// A pinned watch retains its scope across active-session changes and then
	// clears telemetry when its own recording is removed (retention semantics).
	db.SetActiveSession(other.ID, other.Label)
	hub.Broadcast(liveevent.NewActiveSessionEvent(&liveevent.ActiveSessionPayload{ID: other.ID, Label: other.Label}))
	send("tab")
	wait("GET /orders")
	send("enter")
	wait("Missed inventory lookup")
	capture("late-span-waterfall")
	send("back")
	if err := db.DeleteSession(selected.ID); err != nil {
		t.Fatal(err)
	}
	wait("No traces in this scope")
	capture("session-removed")
}
