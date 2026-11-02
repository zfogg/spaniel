package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/ws"
)

const traceID = "11111111111111111111111111111111"
const spanID = "2222222222222222"

type fixture struct {
	mu       sync.Mutex
	rules    []apigen.AlertRule
	events   []apigen.AlertEvent
	silences []apigen.AlertSilence
	failures map[string]int
	requests []*url.URL
	methods  []string
	hub      *ws.Hub
}

func exampleRule(t *testing.T, state string) apigen.AlertRule {
	t.Helper()
	data := map[string]any{"id": "latency", "name": "Checkout latency", "enabled": true, "severity": "critical",
		"query_sql":      "SELECT service_name, max(duration_ns) / 1e6 AS value FROM spans GROUP BY service_name",
		"condition_json": `{"kind":"threshold","operator":">","value":500}`,
		"group_by_json":  `["service_name"]`, "annotations_json": `{"runbook":"https://example.com/runbooks/checkout"}`,
		"last_evaluated_at": int64(1793640000000000000), "last_success_at": int64(1793640000000000000), "last_duration_ns": 1200000,
		"pending_for_ns": 30000000000, "cooldown_ns": 60000000000, "query_version": 1,
		"instances": []any{map[string]any{"rule_id": "latency", "group_key": "checkout", "state": state, "value": 842.7,
			"last_evaluated_at": int64(1793640000000000000), "labels_json": fmt.Sprintf(`{"service_name":"checkout","trace_id":"%s","span_id":"%s"}`, traceID, spanID)},
			map[string]any{"rule_id": "latency", "group_key": "payments", "state": "resolved", "value": 121.4, "labels_json": `{"service_name":"payments"}`}}}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var rule apigen.AlertRule
	if err = json.Unmarshal(b, &rule); err != nil {
		t.Fatal(err)
	}
	return rule
}
func newFixture(t *testing.T) (*fixture, protocol.Env) {
	t.Helper()
	f := &fixture{rules: []apigen.AlertRule{exampleRule(t, "firing")}, failures: make(map[string]int), hub: ws.NewHub()}
	f.events = []apigen.AlertEvent{{Id: "e3", RuleId: "latency", GroupKey: "checkout", Kind: "firing", State: "firing", CreatedAt: 1793640000000000000, Detail: "Latency exceeded 500ms"},
		{Id: "e2", RuleId: "latency", GroupKey: "checkout", Kind: "pending", State: "pending", CreatedAt: 1793639970000000000},
		{Id: "e1", RuleId: "latency", GroupKey: "payments", Kind: "resolved", State: "resolved", CreatedAt: 1793639940000000000}}
	f.silences = []apigen.AlertSilence{{Id: "s1", RuleId: "latency", GroupKey: "payments", StartsAt: 1793639900000000000, EndsAt: 1793643500000000000, Comment: "Scheduled maintenance"}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api, err := client.New(srv.URL, client.Options{MinBackoff: time.Millisecond, MaxBackoff: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	refresh := client.NewRefreshes(ctx, api)
	refresh.SetSession("session-a")
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, method := range f.methods {
			if method != http.MethodGet {
				t.Errorf("mutation request: %s", method)
			}
		}
	})
	return f, protocol.Env{Context: ctx, API: api, Refresh: refresh, Owner: "alerts"}
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		f.hub.ServeWS(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u := *r.URL
	f.requests = append(f.requests, &u)
	f.methods = append(f.methods, r.Method)
	if code := f.failures[r.URL.Path]; code != 0 {
		http.Error(w, `{"error":"fixture failure"}`, code)
		return
	}
	var data any
	total := 0
	switch r.URL.Path {
	case "/api/alerts":
		rules := []apigen.AlertRule{}
		for _, rule := range f.rules {
			if strings.Contains(strings.ToLower(rule.Name+" "+rule.QuerySql), strings.ToLower(r.URL.Query().Get("search"))) {
				rules = append(rules, rule)
			}
		}
		total = len(rules)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		start := min(len(rules), (page-1)*pageSize)
		end := min(len(rules), start+pageSize)
		rules = rules[start:end]
		data = map[string]any{"items": rules}
	case "/api/alerts/latency":
		if len(f.rules) == 0 {
			http.Error(w, `{"error":"alert not found"}`, 404)
			return
		}
		data = f.rules[0]
		total = 1
	case "/api/alerts/latency/events", "/api/alerts/history":
		rows := []apigen.AlertEvent{}
		for _, e := range f.events {
			q := r.URL.Query()
			if (q.Get("group_key") == "" || e.GroupKey == q.Get("group_key")) && (q.Get("state") == "" || e.State == q.Get("state")) && (q.Get("kind") == "" || e.Kind == q.Get("kind")) && (q.Get("search") == "" || strings.Contains(e.Detail, q.Get("search"))) {
				rows = append(rows, e)
			}
		}
		total = len(rows)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		start := min(len(rows), (page-1)*pageSize)
		end := min(len(rows), start+pageSize)
		data = rows[start:end]
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]any{"total": total, "page": 1}, "silences": f.silences})
}
func drain(t *testing.T, env protocol.Env, models ...*model) {
	t.Helper()
	now := time.Now().Add(time.Hour)
	for _, work := range env.Refresh.Due(now) {
		r := work.Run()
		if !env.Refresh.Complete(r, now) {
			continue
		}
		r.Pending = env.Refresh.Pending(r.Key)
		for _, m := range models {
			m.Update(r)
		}
	}
}
func press(m *model, key string) tea.Cmd {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg.Type = tea.KeyEnter
	case "esc":
		msg.Type = tea.KeyEsc
	case "down":
		msg.Type = tea.KeyDown
	case "up":
		msg.Type = tea.KeyUp
	case "end":
		msg.Type = tea.KeyEnd
	default:
		msg.Type = tea.KeyRunes
		msg.Runes = []rune(key)
	}
	_, cmd := m.Update(msg)
	return cmd
}
func invalidate(env protocol.Env, m *model) {
	event, _ := liveevent.Decode([]byte(`{"type":"resources_changed","payload":{"resources":["alerts"],"reason":"evaluation"}}`))
	env.Refresh.Observe(event)
	m.Update(protocol.Live{Event: event})
	// Resync advances generation and makes the next request immediately due,
	// avoiding wall-clock sleeps while exercising the production scheduler.
	env.Refresh.Resync()
}
func assertContains(t *testing.T, m *model, parts ...string) {
	t.Helper()
	view := ansi.Strip(m.View())
	for _, part := range parts {
		if !strings.Contains(view, part) {
			t.Fatalf("missing %q in:\n%s", part, view)
		}
	}
}

func TestStartupFiringAndReadOnlyDetails(t *testing.T) {
	_, env := newFixture(t)
	m := New(env).(*model)
	assertContains(t, m, "Loading list")
	drain(t, env, m)
	assertContains(t, m, "Checkout latency", "firing / critical", "2 instances", "1 firing", "1 resolved")
	show := press(m, "enter")().(protocol.ShowDetail)
	if show.Return.Selection != "latency" {
		t.Fatal(show.Return)
	}
	detailEnv := env
	detailEnv.Owner = "detail:1"
	d := show.New(detailEnv).(*model)
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	d.Update(protocol.Connection{Connected: true})
	d.Update(protocol.SessionChanged{ID: "session-a"})
	drain(t, env, d)
	assertContains(t, d, "Condition / threshold", `"value":500`, "SELECT service_name", "Pending 30s", "Scheduled maintenance")
	press(d, "i")
	assertContains(t, d, "checkout", "payments", "842.7")
	press(d, "g")
	drain(t, env, d)
	assertContains(t, d, "group checkout", "2 events", "Latency exceeded")
	for _, event := range d.events {
		if event.GroupKey != "checkout" {
			t.Fatal("group filter lost")
		}
	}
	press(d, "l")
	d.reconcile()
	var nav tea.Msg
	for _, link := range d.links() {
		if link.span != "" {
			d.link = link.key()
			nav = press(d, "enter")()
			break
		}
	}
	target, ok := nav.(protocol.OpenSpan)
	if !ok || target.TraceID != traceID || target.SpanID != spanID || target.Return.Tab != protocol.Alerts {
		t.Fatalf("navigation: %#v", nav)
	}
	selected := d.instance
	d.Update(protocol.ReturnContext{Tab: protocol.Alerts, Selection: "latency/links"})
	if d.instance != selected || d.group != "checkout" {
		t.Fatal("Back lost detail context")
	}
}
func TestHiddenLifecycleTransitionsDefinitionsAndRemoval(t *testing.T) {
	f, env := newFixture(t)
	m := New(env).(*model)
	drain(t, env, m)
	de := env
	de.Owner = "detail:1"
	d := newDetail(de, "latency").(*model)
	drain(t, env, d)
	m.Update(protocol.Focus{Active: false})
	d.Update(protocol.Focus{Active: false})
	for _, state := range []string{"pending", "firing", "resolved", "error"} {
		f.mu.Lock()
		f.rules[0].Instances[0].State = state
		f.rules[0].Name = "External " + state
		f.events = append([]apigen.AlertEvent{{Id: "transition-" + state, RuleId: "latency", GroupKey: "checkout", Kind: state, State: state}}, f.events...)
		f.rules[0].Instances[0].Value = new(float64)
		*f.rules[0].Instances[0].Value = 17
		f.mu.Unlock()
		invalidate(env, m)
		drain(t, env, m, d)
		if ruleState(m.rules[0]) != state || ruleState(d.rule) != state || d.rule.Name != "External "+state {
			t.Fatalf("hidden %s update lost", state)
		}
		if len(d.events) == 0 || d.events[0].State != state {
			t.Fatalf("hidden %s history update lost", state)
		}
	}
	f.mu.Lock()
	f.rules = nil
	f.events = nil
	f.mu.Unlock()
	invalidate(env, m)
	drain(t, env, m, d)
	assertContains(t, m, "No alert rules")
	m.Update(protocol.ReturnContext{Tab: protocol.Alerts, Selection: "latency"})
	if m.selected != "" || press(m, "enter") != nil {
		t.Fatal("Back restored a deleted row")
	}
	assertContains(t, d, "Rule deleted or unavailable")
	if !d.deleted() {
		t.Fatal("deleted rule not detected")
	}
	press(d, "h")
	assertContains(t, d, "No event history")
	press(d, "t")
	if press(d, "enter") != nil {
		t.Fatal("stale deleted link opened")
	}
}
func TestFailuresReconnectSessionAndGuards(t *testing.T) {
	f, env := newFixture(t)
	m := New(env).(*model)
	drain(t, env, m)
	m.Update(protocol.Connection{Connected: false})
	assertContains(t, m, "snapshots are stale", "Checkout latency")
	f.mu.Lock()
	f.failures["/api/alerts"] = 500
	f.mu.Unlock()
	invalidate(env, m)
	drain(t, env, m)
	assertContains(t, m, "Error loading list", "showing stale data", "Checkout latency")
	f.mu.Lock()
	delete(f.failures, "/api/alerts")
	f.rules[0].Name = "Repaired after reconnect"
	f.mu.Unlock()
	env.Refresh.Resync()
	m.Update(protocol.Connection{Connected: true, Resync: true})
	drain(t, env, m)
	assertContains(t, m, "Repaired after reconnect")
	r := protocol.Result{Key: client.Key{Owner: "other", Name: "list"}, Session: "session-a", Sequence: 999, Data: client.Envelope[apigen.AlertList]{}}
	m.Update(r)
	assertContains(t, m, "Repaired after reconnect")
	r.Key.Owner = "alerts"
	r.Sequence = 1
	m.Update(r)
	assertContains(t, m, "Repaired after reconnect")
	env.Refresh.SetSession("session-b")
	m.Update(protocol.SessionChanged{ID: "session-b"})
	if len(m.rules) != 0 || m.selected != "" {
		t.Fatal("old session retained")
	}
	r.Sequence = 1000
	m.Update(r)
	if len(m.rules) != 0 {
		t.Fatal("late old-session result accepted")
	}
	drain(t, env, m)
	assertContains(t, m, "Repaired after reconnect")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests[len(f.requests)-1].Query().Get("sessionId") != "session-b" {
		t.Fatal("wrong session")
	}
}
func TestFiltersStableSelectionAndImmutableFetch(t *testing.T) {
	f, env := newFixture(t)
	m := New(env).(*model)
	drain(t, env, m)
	press(m, "s")
	assertContains(t, m, "Checkout latency")
	press(m, "s")
	assertContains(t, m, "No alert rules match")
	press(m, "c")
	drain(t, env, m)
	press(m, "v")
	assertContains(t, m, "Checkout latency")
	press(m, "v")
	assertContains(t, m, "No alert rules match")
	press(m, "c")
	drain(t, env, m)
	press(m, "/")
	if !m.CapturesKeys() {
		t.Fatal("search not captured")
	}
	press(m, "q")
	press(m, "esc")
	if m.CapturesKeys() || m.search != "" {
		t.Fatal("cancel search mutated filter")
	}
	press(m, "/")
	press(m, "Checkout")
	press(m, "enter")
	old := env.Refresh.Due(time.Now().Add(2 * time.Hour))
	press(m, "/")
	press(m, " new")
	var results []client.Result
	for _, w := range old {
		results = append(results, w.Run())
	}
	press(m, "enter")
	for _, r := range results {
		if env.Refresh.Complete(r, time.Now()) {
			t.Fatal("superseded search accepted")
		}
	}
	f.mu.Lock()
	last := f.requests[len(f.requests)-1].Query().Get("search")
	f.mu.Unlock()
	if last != "Checkout" {
		t.Fatalf("worker captured mutable query: %q", last)
	}
	drain(t, env, m)
	if len(m.rules) != 0 {
		t.Fatal("new search lost")
	}
	de := env
	de.Owner = "detail:1"
	d := newDetail(de, "latency").(*model)
	drain(t, env, d)
	press(d, "i")
	press(d, "down")
	if d.instance != "payments" {
		t.Fatal(d.instance)
	}
	f.mu.Lock()
	r := f.rules[0]
	r.Instances[0], r.Instances[1] = r.Instances[1], r.Instances[0]
	f.rules[0] = r
	f.mu.Unlock()
	invalidate(env, d)
	drain(t, env, d)
	if d.instance != "payments" {
		t.Fatal("reorder lost stable group selection")
	}
	press(d, "h")
	press(d, "s")
	drain(t, env, d)
	if len(d.events) != 1 || d.events[0].State != "pending" {
		t.Fatal("history state filter")
	}
	press(d, "/")
	press(d, "nothing")
	press(d, "enter")
	drain(t, env, d)
	assertContains(t, d, "No event history matches")
}
func TestPartialFailureResizeAndSafeLinks(t *testing.T) {
	f, env := newFixture(t)
	env.Owner = "detail:1"
	f.mu.Lock()
	f.failures["/api/alerts/history"] = 500
	f.rules[0].Name = "Unsafe\x1b]8;;https://bad.example\aTitle\x1b]8;;\a"
	f.mu.Unlock()
	d := newDetail(env, "latency").(*model)
	drain(t, env, d)
	assertContains(t, d, "SELECT service_name")
	press(d, "h")
	assertContains(t, d, "Error loading history")
	for _, section := range []string{"definition", "instances", "history", "links"} {
		d.section = section
		for _, size := range []tea.WindowSizeMsg{{Width: 1, Height: 1}, {Width: 18, Height: 5}, {Width: 60, Height: 12}, {Width: 120, Height: 30}} {
			d.Update(size)
			view := d.View()
			if strings.Contains(view, "\x1b]8") {
				t.Fatal("unsafe OSC hyperlink")
			}
			if len(strings.Split(view, "\n")) > size.Height {
				t.Fatal("height overflow")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatal("width overflow")
				}
			}
		}
	}
	for _, raw := range []string{"/traces/missing", "//evil.example/traces/" + traceID, "javascript:alert(1)", "https://example.com/traces/" + traceID} {
		l := parseLink(raw, "test")
		d.rule.AnnotationsJson = fmt.Sprintf(`{"link":%q}`, raw)
		d.instance = ""
		d.event = ""
		d.section = "links"
		d.link = l.key()
		if press(d, "enter") != nil {
			t.Errorf("unsafe target opened %q", raw)
		}
	}
	d.rule.AnnotationsJson = `{"link":"/traces/` + traceID + `"}`
	d.link = ""
	d.reconcile()
	if nav, ok := press(d, "enter")().(protocol.OpenTrace); !ok || nav.TraceID != traceID {
		t.Fatal("trace navigation failed")
	}
}

func TestPagingClampAndAcknowledgment(t *testing.T) {
	f, env := newFixture(t)
	f.mu.Lock()
	for i := 1; i < 31; i++ {
		r := exampleRule(t, "pending")
		r.Id = fmt.Sprintf("rule-%02d", i)
		r.Name = r.Id
		f.rules = append(f.rules, r)
	}
	f.mu.Unlock()
	m := New(env).(*model)
	drain(t, env, m)
	if len(m.rules) != pageSize || m.total != 31 {
		t.Fatalf("first page %d/%d", len(m.rules), m.total)
	}
	press(m, "]")
	drain(t, env, m)
	if m.page != 2 || len(m.rules) != 6 || m.selected != "rule-25" {
		t.Fatalf("second page %#v", m)
	}
	press(m, "end")
	if m.selected != "rule-30" {
		t.Fatal("page selection")
	}
	f.mu.Lock()
	f.rules = f.rules[:1]
	f.mu.Unlock()
	invalidate(env, m)
	drain(t, env, m)
	drain(t, env, m)
	if m.page != 1 || m.selected != "latency" {
		t.Fatal("deletion did not repair page")
	}
	r := exampleRule(t, "firing")
	at := int64(1793640000000000000)
	r.Instances[0].AcknowledgedAt = &at
	r.Instances[0].AcknowledgementNote = "Investigating checkout"
	if ruleState(r) != "acknowledged" {
		t.Fatal("acknowledgment not represented")
	}
	f.mu.Lock()
	f.rules = []apigen.AlertRule{r}
	f.mu.Unlock()
	de := env
	de.Owner = "detail:1"
	d := newDetail(de, "latency").(*model)
	drain(t, env, d)
	press(d, "i")
	show := press(d, "enter")().(protocol.ShowDetail)
	de.Owner = "detail:2"
	inspection := show.New(de).(*model)
	drain(t, env, inspection)
	inspection.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	assertContains(t, inspection, "State: acknowledged / condition: firing", "Investigating checkout", "Scheduled maintenance")
	r.Enabled = false
	if ruleState(r) != "disabled" {
		t.Fatal("disabled rule masquerades as firing")
	}
}
