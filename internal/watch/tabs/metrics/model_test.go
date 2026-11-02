package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/shell"
)

func testEnv(t *testing.T) protocol.Env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api, _ := client.New("http://127.0.0.1:1", client.Options{})
	refresh := client.NewRefreshes(ctx, api)
	refresh.SetSession("seed")
	return protocol.Env{Context: ctx, API: api, Refresh: refresh, Owner: "metrics"}
}
func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func streamRow(row *liveevent.MetricRowPayload, only bool) protocol.Live {
	return protocol.Live{Event: liveevent.Message{Type: "metric", Decoded: &liveevent.MetricPayload{Name: row.Name, ServiceName: row.ServiceName, Type: row.Type, CatalogOnly: only, Row: row}}}
}
func catalogResult(m *catalog, rows ...apigen.MetricCatalogEntry) protocol.Result {
	return protocol.Result{Key: client.Key{Owner: m.env.Owner, Name: "catalog"}, Session: m.session, Data: client.Envelope[[]apigen.MetricCatalogEntry]{Data: rows}}
}
func TestCatalogNavigationSearchReturnAndLiveScope(t *testing.T) {
	m := New(testEnv(t)).(*catalog)
	rows := []apigen.MetricCatalogEntry{{Name: "cpu", ServiceName: "api", Type: "gauge", Unit: "%", Description: "CPU usage", SampleCount: 10}, {Name: "requests", ServiceName: "web", Type: "sum", Unit: "{request}", SampleCount: 20}}
	m.Update(catalogResult(m, rows...))
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	wanted := identity(rows[1])
	if m.selected != wanted {
		t.Fatal("down did not select second stream")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	show, ok := cmd().(protocol.ShowDetail)
	if !ok || show.Return.Selection != wanted {
		t.Fatal("missing stable Back context")
	}
	// New/reordered catalogs preserve identity rather than row offset.
	m.Update(catalogResult(m, append([]apigen.MetricCatalogEntry{{Name: "aaa", ServiceName: "api"}}, rows...)...))
	if m.selected != wanted {
		t.Fatal("snapshot shifted selection")
	}
	m.Update(key("/"))
	m.Update(key("service=api type=gauge"))
	if !m.CapturesKeys() || len(m.filtered()) != 1 {
		t.Fatal("search qualifiers failed")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.CapturesKeys() {
		t.Fatal("escape did not release shell keys")
	}
	m.Update(protocol.ReturnContext{Tab: protocol.Metrics, Selection: wanted})
	if m.selected != wanted {
		t.Fatal("return context lost")
	}
	row := &liveevent.MetricRowPayload{Name: "new.live", ServiceName: "api", Type: "gauge", TimestampNs: time.Now().UnixNano(), SessionID: "other"}
	m.Update(streamRow(row, false))
	if len(m.rows) != 3 {
		t.Fatal("cross-session row leaked")
	}
	row.SessionID = "seed"
	event := streamRow(row, true)
	m.env.Refresh.Observe(event.Event)
	m.Update(event)
	m.Update(event)
	if len(m.rows) != 4 {
		t.Fatal("live addition missing or duplicated")
	}
	for _, r := range m.rows {
		if r.Name == row.Name && r.SampleCount != 0 {
			t.Fatal("fabricated aggregate count")
		}
	}
	m.Update(protocol.SessionChanged{ID: "next"})
	if len(m.rows) != 0 || m.selected != "" || m.loaded {
		t.Fatal("old-session catalog retained")
	}
}
func TestDetailHiddenLifecycleFilteringWindowsAndErrors(t *testing.T) {
	env := testEnv(t)
	env.Owner = "detail:1"
	m := newDetail(env, apigen.MetricCatalogEntry{Name: "cpu", ServiceName: "api", Type: "gauge", Unit: "%"})
	now := m.now
	data := seriesData{Type: "gauge", Unit: "%", Series: []labeledSeries{{Key: "a", Attributes: map[string]any{"http.route": "/cart"}, Points: []sample{{Timestamp: now.Add(-time.Second).UnixNano(), Value: 4}}}, {Key: "b", Attributes: map[string]any{"http.route": "/checkout"}, Points: []sample{{Timestamp: now.Add(-time.Second).UnixNano(), Value: 8}}}}}
	result := protocol.Result{Key: m.key, Session: "seed", Data: seriesSnapshot{Data: data, To: now}}
	m.Update(protocol.Focus{Active: false})
	m.Update(result)
	if !m.loaded || len(m.data.Series) != 2 {
		t.Fatal("hidden HTTP result lost")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.selected != "b" {
		t.Fatal("series navigation failed")
	}
	event := streamRow(&liveevent.MetricRowPayload{Name: "cpu", ServiceName: "api", Type: "gauge", Unit: "%", TimestampNs: now.UnixNano(), SessionID: "seed", SeriesKey: "c", SeriesAttributes: `{"http.route":"/new"}`, Value: 12}, true)
	m.Update(event)
	m.Update(event)
	if m.selected != "b" || len(m.data.Series) != 3 || len(m.data.Series[2].Points) != 1 {
		t.Fatal("hidden live update duplicated or selection changed")
	}
	m.Update(key("/"))
	m.Update(key("http.route=/new"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.candidates()) != 1 || m.selected != "c" {
		t.Fatal("label filter failed")
	}
	m.Update(protocol.Result{Key: m.key, Session: "seed", Err: errors.New("test unavailable")})
	if !strings.Contains(m.View(), "test unavailable") || !strings.Contains(m.View(), "stale") {
		t.Fatal("error did not retain stale data")
	}
	m.Update(protocol.Connection{Connected: false})
	m.Update(tea.WindowSizeMsg{Width: 27, Height: 12})
	for _, line := range strings.Split(m.View(), "\n") {
		if ansi.StringWidth(line) > 27 {
			t.Fatalf("narrow overflow: %q", line)
		}
	}
	oldKey := m.key
	selected, replacement := m.selected, m.data
	m.Update(key("w"))
	if m.key == oldKey || m.loaded {
		t.Fatal("time range did not supersede snapshot")
	}
	m.Update(result)
	if m.loaded {
		t.Fatal("obsolete range response accepted")
	}
	m.Update(protocol.Tick{Time: now.Add(time.Second)})
	if m.selected != selected {
		t.Fatal("loading window lost series selection")
	}
	m.Update(protocol.Result{Key: m.key, Session: "seed", Data: seriesSnapshot{Data: replacement, To: now.Add(time.Second)}})
	if m.selected != selected {
		t.Fatal("new window did not preserve label identity")
	}
	m.Update(protocol.Tick{Time: now.Add(25 * time.Hour)})
	if len(m.data.Series) != 0 {
		t.Fatal("idle data did not expire")
	}
	m.Update(protocol.SessionChanged{ID: "next"})
	m.Update(protocol.Result{Key: m.key, Session: "seed", Data: seriesSnapshot{Data: data}})
	if m.loaded || len(m.data.Series) != 0 {
		t.Fatal("old-session result accepted")
	}
}
func TestSelfTelemetryDoesNotScheduleCatalogRefetch(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*catalog)
	now := time.Now()
	work := env.Refresh.Due(now)
	if len(work) != 1 {
		t.Fatal("missing initial request")
	}
	if !env.Refresh.Complete(work[0].Result, now) {
		t.Fatal("initial completion rejected")
	}
	event := streamRow(&liveevent.MetricRowPayload{Name: "self.calls", ServiceName: "spaniel", Type: "sum", TimestampNs: now.UnixNano(), SessionID: "seed"}, true)
	env.Refresh.Observe(event.Event)
	m.Update(event)
	if len(env.Refresh.Due(now.Add(10*time.Second))) != 0 {
		t.Fatal("catalogOnly created an HTTP loop")
	}
	if len(m.rows) != 1 {
		t.Fatal("catalogOnly row patch lost")
	}
}
func TestHTTPQueryWindowSupersessionAndIdleRefresh(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	queries := make(chan *http.Request, 4)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"name":"cpu","service_name":"api","type":"gauge","unit":"%","series":[],"points":[],"traces":[]}}`))
	}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	env := testEnv(t)
	api, _ := client.New(srv.URL, client.Options{})
	env.API = api
	env.Refresh = client.NewRefreshes(env.Context, api)
	env.Refresh.SetSession("seed")
	m := newDetail(env, apigen.MetricCatalogEntry{Name: "cpu", ServiceName: "api", Type: "gauge"})
	work := env.Refresh.Due(time.Now())
	if len(work) != 1 {
		t.Fatal("initial series request missing")
	}
	result := work[0].Run()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	req := <-queries
	q := req.URL.Query()
	from, _ := time.ParseDuration("15m")
	snap := result.Data.(seriesSnapshot)
	if q.Get("sessionId") != "seed" || q.Get("name") != "cpu" || q.Get("service") != "api" || q.Get("operation") != "raw" || snap.To.Sub(snap.From) != from {
		t.Fatalf("bad scope/window %v", q)
	}
	env.Refresh.Complete(result, time.Now())
	m.Update(result)
	if !m.loaded || !strings.Contains(m.View(), "No samples") {
		t.Fatal("empty initial response not shown")
	}
	if len(env.Refresh.Due(time.Now().Add(6*time.Second))) != 1 {
		t.Fatal("idle window did not refresh")
	}
	env.Refresh.Resync()
	m.Update(key("w"))
	m.Update(result)
	if m.loaded {
		t.Fatal("replaced request overwrote window")
	}
}

type parked struct{}

func (parked) Init() tea.Cmd                         { return nil }
func (p parked) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (parked) View() string                          { return "Other tab" }
func TestShellRoutesHiddenCatalogAndRetainedDetail(t *testing.T) {
	env := testEnv(t)
	var cat *catalog
	model := shell.New(env, []protocol.Tab{{ID: protocol.Metrics, Title: "metrics", New: func(e protocol.Env) tea.Model { cat = New(e).(*catalog); return cat }}, {ID: protocol.Logs, Title: "logs", New: func(protocol.Env) tea.Model { return parked{} }}}, nil, nil, true)
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	work := env.Refresh.Due(time.Now())
	r := work[0].Result
	r.Data = client.Envelope[[]apigen.MetricCatalogEntry]{Data: []apigen.MetricCatalogEntry{{Name: "cpu", Type: "gauge", ServiceName: "api"}}}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model.Update(r)
	if !cat.loaded {
		t.Fatal("hidden owner result lost")
	}
	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	_, cmd := cat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	show := cmd().(protocol.ShowDetail)
	var inspector *detail
	factory := show.New
	show.New = func(e protocol.Env) tea.Model { inspector = factory(e).(*detail); return inspector }
	model.Update(show)
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	jobs := env.Refresh.Due(time.Now())
	if len(jobs) != 1 {
		t.Fatalf("detail registrations = %d", len(jobs))
	}
	r = jobs[0].Result
	r.Data = seriesSnapshot{To: time.Now(), Data: seriesData{Type: "gauge", Series: []labeledSeries{{Key: "one", Attributes: map[string]any{}, Points: []sample{{Timestamp: time.Now().UnixNano(), Value: 8}}}}}}
	model.Update(r)
	if !inspector.loaded {
		t.Fatal("hidden retained detail result lost")
	}
	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cat.selected != show.Return.Selection {
		t.Fatal("Back context lost")
	}
	if len(env.Refresh.Due(time.Now().Add(time.Minute))) != 0 {
		t.Fatal("closed detail subscription leaked")
	}
}
