package metrics

import (
	"bytes"
	"context"
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
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/shell"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type rendererOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *rendererOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}
func (o *rendererOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }
func seedMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "checkout-api")
	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("checkout-sdk")
	counter := sm.Metrics().AppendEmpty()
	counter.SetName("checkout.requests")
	counter.SetDescription("Completed checkout requests by HTTP route")
	counter.SetUnit("{request}")
	sum := counter.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sum.SetIsMonotonic(true)
	gauge := sm.Metrics().AppendEmpty()
	gauge.SetName("process.cpu")
	gauge.SetDescription("Worker CPU utilization")
	gauge.SetUnit("%")
	g := gauge.SetEmptyGauge()
	histogram := sm.Metrics().AppendEmpty()
	histogram.SetName("http.server.duration")
	histogram.SetDescription("Checkout request latency with explicit OTLP buckets")
	histogram.SetUnit("ms")
	h := histogram.SetEmptyHistogram()
	h.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	start := pcommon.NewTimestampFromTime(time.Now().Add(-time.Minute))
	for i := 0; i < 12; i++ {
		ts := pcommon.NewTimestampFromTime(time.Now().Add(time.Duration(i-15) * time.Second))
		for j, route := range []string{"/cart", "/checkout"} {
			p := sum.DataPoints().AppendEmpty()
			p.SetTimestamp(ts)
			p.SetStartTimestamp(start)
			p.SetDoubleValue(float64(1000 + i*i*3 + j*120))
			p.Attributes().PutStr("http.route", route)
			hp := h.DataPoints().AppendEmpty()
			hp.SetTimestamp(ts)
			hp.SetStartTimestamp(pcommon.Timestamp(uint64(ts) - 1e9))
			hp.SetCount(10)
			hp.SetSum(float64(400 + i*30 + j*150))
			hp.ExplicitBounds().FromRaw([]float64{10, 25, 50, 100, 250})
			hp.BucketCounts().FromRaw([]uint64{1, 2, 3, uint64(3 - i%3), uint64(1 + i%3), 0})
			hp.Attributes().PutStr("http.route", route)
		}
		p := g.DataPoints().AppendEmpty()
		p.SetTimestamp(ts)
		p.SetDoubleValue(float64(25 + i*3))
		p.Attributes().PutStr("operation", "checkout")
	}
	exp := sm.Metrics().AppendEmpty()
	exp.SetName("rpc.payload.distribution")
	exp.SetDescription("Payload sizes as an exponential histogram")
	exp.SetUnit("By")
	eh := exp.SetEmptyExponentialHistogram()
	eh.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	ep := eh.DataPoints().AppendEmpty()
	ep.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-time.Second)))
	ep.SetCount(4)
	ep.SetSum(1200)
	ep.SetScale(2)
	ep.Positive().SetOffset(20)
	ep.Positive().BucketCounts().FromRaw([]uint64{1, 1, 2})
	summary := sm.Metrics().AppendEmpty()
	summary.SetName("rpc.payload.summary")
	summary.SetDescription("Exporter-provided payload quantiles")
	summary.SetUnit("By")
	sp := summary.SetEmptySummary().DataPoints().AppendEmpty()
	sp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-time.Second)))
	sp.SetCount(10)
	sp.SetSum(4000)
	q := sp.QuantileValues().AppendEmpty()
	q.SetQuantile(.95)
	q.SetValue(800)
	return md
}
func TestRealIngestTerminalMetrics(t *testing.T) {
	captureDir := os.Getenv("SPANIEL_WATCH_METRICS_CAPTURE_DIR")
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
	session, err := db.CreateSession("Metrics terminal verification", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	hub := ws.NewHub()
	pipeline := ingestion.NewPipeline(db, hub)
	defer pipeline.Close(context.Background())
	if err := pipeline.IngestMetrics(context.Background(), seedMetrics()); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sockets := make(chan net.Conn, 4)
	srv := httptest.NewUnstartedServer(api.NewRouter(db, hub, nil))
	srv.Listener.Close()
	srv.Listener = ln
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiClient, err := client.New(srv.URL, client.Options{MinBackoff: 100 * time.Millisecond, MaxBackoff: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	updates, err := apiClient.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refresh := client.NewRefreshes(ctx, apiClient)
	refresh.SetSession(session.ID)
	model := shell.New(protocol.Env{Context: ctx, API: apiClient, Refresh: refresh}, []protocol.Tab{{ID: protocol.Metrics, Title: "metrics", New: New}, {ID: protocol.Logs, Title: "logs", New: func(protocol.Env) tea.Model { return parked{} }}}, nil, updates, true)
	var output rendererOutput
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(ctx), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer program.Kill()
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 38})
	waitText := func(want string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for !strings.Contains(output.text(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("terminal never rendered %q\n%s", want, output.text())
			}
			time.Sleep(10 * time.Millisecond)
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
		if err := os.WriteFile(filepath.Join(captureDir, name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	waitText("5 matching streams")
	waitText("rpc.payload.summary")
	capture("metrics-catalog")
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText("Authoritative samples")
	program.Send(press("o"))
	program.Send(press("o"))
	waitText("view rate")
	for range 4 {
		program.Send(press("w"))
	}
	waitText("window 1m0s")
	time.Sleep(400 * time.Millisecond)
	capture("metrics-counter-rate")
	// Retain this detail on another tab while new series/catalog rows arrive.
	program.Send(tea.KeyMsg{Type: tea.KeyTab})
	live := pmetric.NewMetrics()
	rm := live.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "checkout-api")
	metric := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("checkout.requests")
	metric.SetUnit("{request}")
	sum := metric.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sum.SetIsMonotonic(true)
	p := sum.DataPoints().AppendEmpty()
	p.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	p.SetDoubleValue(450)
	p.Attributes().PutStr("http.route", "/refund")
	queue := rm.ScopeMetrics().At(0).Metrics().AppendEmpty()
	queue.SetName("worker.queue.depth")
	queue.SetUnit("{item}")
	qp := queue.SetEmptyGauge().DataPoints().AppendEmpty()
	qp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	qp.SetIntValue(7)
	if err := pipeline.IngestMetrics(ctx, live); err != nil {
		t.Fatal(err)
	}
	program.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	program.Send(press("/"))
	program.Send(press("http.route=/refund"))
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText("/refund")
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitText("6 matching streams")
	program.Send(tea.KeyMsg{Type: tea.KeyDown})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText("view p95")
	for range 4 {
		program.Send(press("w"))
	}
	time.Sleep(400 * time.Millisecond)
	capture("metrics-histogram")
	// A real transport failure must recover authoritative selected data.
	beforeFailure := len(output.text())
	select {
	case c := <-sockets:
		c.Close()
	case <-time.After(time.Second):
		t.Fatal("no shared socket")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(output.text()[beforeFailure:], "reconnecting") {
		if time.Now().After(deadline) {
			t.Fatal("transport failure not rendered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	beforeRecovery := len(output.text())
	for !strings.Contains(output.text()[beforeRecovery:], "live") {
		if time.Now().After(deadline) {
			t.Fatal("fresh reconnect recovery not rendered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	program.Send(tea.KeyMsg{Type: tea.KeyDown})
	program.Send(tea.KeyMsg{Type: tea.KeyDown})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText("Distribution table")
	capture("metrics-exponential")
	program.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal failed to exit")
	}
}
