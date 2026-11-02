package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

func definition(t *testing.T, raw string) apigen.Dashboard {
	t.Helper()
	var d apigen.Dashboard
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

const example = `{"id":"demo","name":"Checkout","source_file":"dashboards/checkout.yaml","description":"Requests and latency","panels":[{"id":"p2","title":"Rows","position":2,"display_type":"table","query_sql":"SELECT $service AS value","settings_json":"{}"},{"id":"p1","title":"Requests","position":1,"display_type":"single_value","query_sql":"SELECT $service AS value","settings_json":"{\"unit\":\"requests\"}"}],"variables":[{"name":"service","kind":"string","default_value":"api","options_json":"[]"}]}`

func envFor(t *testing.T, handler http.Handler) (protocol.Env, *client.Refreshes) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api, err := client.New(srv.URL, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := client.NewRefreshes(ctx, api)
	return protocol.Env{Context: ctx, API: api, Refresh: r, Owner: "dashboards"}, r
}
func deliver(m *model, r *client.Refreshes, now time.Time) int {
	work := r.Due(now)
	for _, w := range work {
		result := w.Run()
		if r.Complete(result, now) {
			m.Update(result)
		}
	}
	return len(work)
}
func snapshot(m *model, d ...apigen.Dashboard) {
	encoded, _ := json.Marshal(d)
	var clone []apigen.Dashboard
	_ = json.Unmarshal(encoded, &clone)
	d = clone
	m.Update(protocol.Result{Key: client.Key{Owner: m.env.Owner, Name: "list"}, Sequence: m.listSequence + 1, Data: client.Envelope[[]apigen.Dashboard]{Data: d}})
}
func key(m *model, k string) {
	typ := tea.KeyRunes
	var runes []rune
	switch k {
	case "enter":
		typ = tea.KeyEnter
	case "esc":
		typ = tea.KeyEsc
	default:
		runes = []rune(k)
	}
	m.Update(tea.KeyMsg{Type: typ, Runes: runes})
}
func TestDiscoveryNavigationAndReadOnlyRequests(t *testing.T) {
	d := definition(t, example)
	var bodies []apigen.PreviewDashboardQueryJSONBody
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/dashboards":
			data = []apigen.Dashboard{d}
		case r.Method == "GET" && r.URL.Path == "/api/sessions/active":
			data = apigen.ActiveSession{Id: "session-a"}
		case r.Method == "POST" && r.URL.Path == "/api/dashboards/demo/query-preview":
			var body apigen.PreviewDashboardQueryJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			bodies = append(bodies, body)
			data = apigen.QueryPreview{Columns: []string{"value"}, Rows: []map[string]any{{"value": (*body.Variables)["service"]}}}
		default:
			t.Errorf("unexpected/mutation request %s %s", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	env, r := envFor(t, handler)
	m := New(env).(*model)
	now := time.Now()
	deliver(m, r, now)
	if !strings.Contains(m.View(), "checkout.yaml") || m.selected != "demo" {
		t.Fatal(m.View())
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	open, ok := cmd().(protocol.ShowDetail)
	if !ok || open.Return.Selection != "demo" {
		t.Fatal("missing stable Back context")
	}
	env.Owner = "detail:1"
	detail := open.New(env).(*model)
	detail.Update(protocol.SessionChanged{ID: "session-a"})
	detail.Update(protocol.Connection{Connected: true})
	deliver(detail, r, now)
	deliver(detail, r, now)
	if detail.panelID != "p1" || !strings.Contains(detail.View(), "value: api requests") {
		t.Fatal(detail.View())
	}
	// A hidden detail continues receiving results; Focus never disables subscriptions.
	detail.Update(protocol.Focus{Active: false})
	key(detail, "e")
	detail.draft = "api'; DROP TABLE spans; --"
	key(detail, "enter")
	deliver(detail, r, now)
	for _, body := range bodies {
		if body.QuerySql != "SELECT $service AS value" {
			t.Fatal("unsafe interpolation", body.QuerySql)
		}
	}
	if !strings.Contains(detail.View(), "DROP TABLE") {
		t.Fatal("hidden result not applied", detail.View())
	}
	detail.Update(protocol.ReturnContext{Tab: protocol.Dashboards, Selection: "demo"})
	if len(bodies) != 4 {
		t.Fatalf("queries=%d", len(bodies))
	}
	// Durable invalidation coalesces and idle queries use the documented five seconds.
	event, _ := liveevent.Decode([]byte(`{"type":"log","payload":{}}`))
	r.Observe(event)
	event, _ = liveevent.Decode([]byte(`{"type":"log","payload":{}}`))
	r.Observe(event)
	if got := deliver(detail, r, now.Add(4*time.Second)); got != 0 {
		t.Fatalf("early interval refresh: %d", got)
	}
	if got := deliver(detail, r, now.Add(5*time.Second)); got != 2 {
		t.Fatalf("missing bounded refresh: %d", got)
	}
}
func TestDefinitionLifecycleStaleGuardsAndSession(t *testing.T) {
	env, r := envFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	m := newModel(env, "demo")
	d := definition(t, example)
	snapshot(m, d)
	oldGeneration := m.generation
	oldName := ""
	for name := range m.panels {
		oldName = name
		break
	}
	key(m, "e")
	m.draft = "worker"
	key(m, "enter")
	if m.generation <= oldGeneration {
		t.Fatal("variable switch did not supersede workers")
	}
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: oldName}, Sequence: 100, Data: client.Envelope[apigen.QueryPreview]{Data: apigen.QueryPreview{Rows: []map[string]any{{"value": "obsolete"}}}}})
	if strings.Contains(m.View(), "obsolete") {
		t.Fatal("stale result accepted")
	}
	// Metadata repair retains overrides and stable panel identity after reorder.
	m.panelID = "p2"
	d.Name = "Renamed"
	d.Panels[0].Position = 0
	snapshot(m, d)
	if m.panelID != "p2" || m.overrides["service"] != "worker" {
		t.Fatal("context lost on external edit")
	}
	name := ""
	for k := range m.panels {
		name = k
		break
	}
	m.Update(protocol.Result{Key: client.Key{Owner: "wrong", Name: name}, Sequence: 99, Err: errors.New("wrong owner")})
	if m.panels[name].err != nil {
		t.Fatal("owner guard")
	}
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: name}, Sequence: 9, Data: client.Envelope[apigen.QueryPreview]{Data: apigen.QueryPreview{Rows: []map[string]any{{"value": 42.0}}}}})
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: name}, Sequence: 8, Err: errors.New("older")})
	if m.panels[name].err != nil {
		t.Fatal("sequence guard")
	}
	snapshot(m)
	if !m.removed || len(m.panels) != 0 || !strings.Contains(m.View(), "removed externally") {
		t.Fatal(m.View())
	}
	// Removal cancels subscriptions, even while hidden, and recreation reconciles.
	snapshot(m, d)
	if m.removed || len(m.panels) != 2 {
		t.Fatal("recreation not loaded")
	}
	d.Panels[0].QuerySql = "SELECT $session_id AS value"
	snapshot(m, d)
	m.session = "pinned"
	m.activeID = "active"
	m.activeLoaded = true
	m.queryPanels()
	state := m.stateFor(d.Panels[0].Id)
	if state.err == nil || !strings.Contains(state.err.Error(), "cannot scope") {
		t.Fatal("wrong-session preview allowed")
	}
	m.Update(protocol.SessionChanged{ID: "active"})
	state = m.stateFor(d.Panels[0].Id)
	if state.err != nil {
		t.Fatal(state.err)
	}
	m.Update(protocol.Connection{Connected: false})
	if !strings.Contains(m.View(), "STALE") {
		t.Fatal("outage not visible")
	}
	// Check all query requests were removed on final deletion (list/active remain).
	snapshot(m)
	if len(r.Due(time.Now())) > 2 {
		t.Fatal("removed panels still subscribed")
	}
}
func TestVariablesEnumLiveClockAndNarrowResize(t *testing.T) {
	env, _ := envFor(t, http.NotFoundHandler())
	m := newModel(env, "demo")
	d := definition(t, example)
	d.Variables[0].Kind = "enum"
	d.Variables[0].OptionsJson = `["api","worker"]`
	snapshot(m, d)
	key(m, "e")
	m.draft = "injection"
	key(m, "enter")
	if !m.CapturesKeys() || m.inputError == "" {
		t.Fatal("invalid enum accepted")
	}
	m.draft = "worker"
	key(m, "enter")
	if m.CapturesKeys() || m.overrides["service"] != "worker" {
		t.Fatal("enum choice failed")
	}
	key(m, "r")
	if len(m.overrides) != 0 {
		t.Fatal("defaults did not reset")
	}
	sql := "SELECT coalesce(greatest((SELECT max(timestamp_ns) FROM telemetry_metrics), (SELECT max(start_ns) FROM telemetry_spans)), epoch_ns(current_timestamp)) AS value"
	if panelSQL(sql, false) != sql || panelSQL(sql, true) != "SELECT epoch_ns(current_timestamp) AS value" {
		t.Fatal(panelSQL(sql, true))
	}
	key(m, "t")
	if !m.live {
		t.Fatal("live mode not selected")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 22, Height: 12}, {Width: 120, Height: 35}, {Width: 1, Height: 1}} {
		m.Update(size)
		view := m.View()
		if len(strings.Split(view, "\n")) > size.Height {
			t.Fatal("height overflow")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("width overflow %q", line)
			}
		}
	}
}
func TestPanelSemantics(t *testing.T) {
	data := apigen.QueryPreview{Columns: []string{"timestamp_ns", "group_value", "value"}, Rows: []map[string]any{{"timestamp_ns": "1793631000000000001", "value": 2.5, "group_value": "api"}, {"timestamp_ns": "1793631060000000001", "value": "3.5", "group_value": "api"}, {"timestamp_ns": nil, "value": 4.0}, {"timestamp_ns": "1793631060000000001", "value": nil}}}
	series, from, to, skipped := timeSeries(data.Rows)
	if len(series) != 1 || series[0].Name != "api" || from.UnixNano() != 1793631000000000001 || to.UnixNano() != 1793631060000000001 || skipped != 2 {
		t.Fatal("series semantics", series, from, to, skipped)
	}
	for _, kind := range []string{"time_series", "single_value", "table", "heatmap", "deploy_correlation"} {
		view := strings.Join(renderPanel(kind, `{"unit":"ms"}`, data, 80, 15, false), "\n")
		if !strings.Contains(view, "ms") {
			t.Fatal(view)
		}
		if kind == "heatmap" && !strings.Contains(view, "unsupported") {
			t.Fatal(view)
		}
		if kind == "time_series" && (!strings.Contains(view, "Series: api") || !strings.Contains(view, "NULL")) {
			t.Fatal(view)
		}
	}
	if got := strings.Join(renderPanel("time_series", "{}", data, 20, 5, false), "\n"); !strings.Contains(got, "too narrow") || !strings.Contains(got, "3.5") {
		t.Fatal(got)
	}
	if got := strings.Join(renderPanel("table", "{}", apigen.QueryPreview{}, 80, 15, false), "\n"); !strings.Contains(got, "No telemetry") {
		t.Fatal(got)
	}
	if got := safe("\x1b[31mred\x1b[0m\n\x07"); got != "red  " {
		t.Fatal("terminal controls leaked", got)
	}
}
func TestLiveDefinitionStatusAndPartialError(t *testing.T) {
	env, _ := envFor(t, http.NotFoundHandler())
	m := newModel(env, "demo")
	snapshot(m, definition(t, example))
	m.Update(protocol.Connection{Connected: true})
	event, err := liveevent.Decode([]byte(`{"type":"definition_sync","payload":{"kind":"dashboard","status":"error","message":"bad YAML"}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(protocol.Live{Event: event})
	if !strings.Contains(m.View(), "bad YAML") {
		t.Fatal(m.View())
	}
	event, err = liveevent.Decode([]byte(`{"type":"definition_sync","payload":{"kind":"dashboard","status":"updated"}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(protocol.Live{Event: event})
	if strings.Contains(m.View(), "bad YAML") {
		t.Fatal("sync error did not clear")
	}
	name := fmt.Sprintf("panel:%d:%s", m.generation, m.panelID)
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: name}, Sequence: 10, Pending: true, Data: client.Envelope[apigen.QueryPreview]{Data: apigen.QueryPreview{Columns: []string{"value"}, Rows: []map[string]any{{"value": 42.0}}}}})
	if view := m.View(); !strings.Contains(view, "pending repair") || !strings.Contains(view, "value: 42") {
		t.Fatal(view)
	}
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: name}, Sequence: 11, Err: errors.New("SQL/type validation failed")})
	if view := m.View(); !strings.Contains(view, "Query error:") || !strings.Contains(view, "STALE · last successful result") || !strings.Contains(view, "value: 42") {
		t.Fatal(view)
	}
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: "list"}, Sequence: m.listSequence + 1, Err: errors.New("definitions unavailable")})
	if view := m.View(); !strings.Contains(view, "definitions unavailable") || !strings.Contains(view, "Requests") {
		t.Fatal(view)
	}
}
func TestPinnedSessionTracksActiveFramesAndResync(t *testing.T) {
	env, _ := envFor(t, http.NotFoundHandler())
	m := newModel(env, "demo")
	d := definition(t, example)
	d.Panels[0].QuerySql = "SELECT $session_id AS value"
	snapshot(m, d)
	m.session = "pinned"
	m.activeID = "pinned"
	m.activeLoaded = true
	m.queryPanels()
	event, err := liveevent.Decode([]byte(`{"type":"active_session","payload":{"id":"other"}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(protocol.Live{Event: event})
	if m.session != "pinned" || m.activeID != "other" || m.stateFor("p2").err == nil {
		t.Fatal("pinned scope silently followed active session")
	}
	generation := m.generation
	m.Update(protocol.Live{Event: event})
	if m.generation != generation {
		t.Fatal("duplicate active frame restarted queries")
	}
	m.Update(protocol.Connection{Connected: true, Resync: true})
	if m.activeLoaded || m.stateFor("p2").loaded {
		t.Fatal("reconnect reused an unverified session")
	}
}
