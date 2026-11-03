package logs

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func testEnv(t *testing.T) protocol.Env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := client.NewRefreshes(ctx, nil)
	r.SetSession("session")
	return protocol.Env{Context: ctx, Owner: "logs", Refresh: r}
}
func logRow(body string, ns int64) apigen.Log {
	return apigen.Log{Body: body, TimestampNs: ns, ReceivedAt: ns + 1, ServiceName: "checkout", SessionId: "session", Severity: 17, TraceId: "0123456789abcdef0123456789abcdef", SpanId: "0123456789abcdef", Attributes: `{"cart.items":3,"exception.message":"payment declined"}`}
}
func pressKey(m tea.Model, value string) tea.Cmd {
	typeOf := tea.KeyRunes
	switch value {
	case "enter":
		typeOf = tea.KeyEnter
	case "esc":
		typeOf = tea.KeyEsc
	case "down":
		typeOf = tea.KeyDown
	case "end":
		typeOf = tea.KeyEnd
	}
	_, cmd := m.Update(tea.KeyMsg{Type: typeOf, Runes: []rune(value)})
	return cmd
}
func snapshot(m tea.Model, owner, name string, rows ...apigen.Log) {
	m.Update(protocol.Result{Key: client.Key{Owner: owner, Name: name}, Data: client.Envelope[[]apigen.Log]{Data: rows}})
}
func event(row apigen.Log, self bool) protocol.Live {
	p := &liveevent.SelfLogPayload{Body: row.Body, TimestampNs: row.TimestampNs, ReceivedAt: row.ReceivedAt, ServiceName: row.ServiceName, SessionID: row.SessionId, Severity: int(row.Severity), TraceID: row.TraceId, SpanID: row.SpanId, Attributes: row.Attributes}
	if self {
		return protocol.Live{Event: liveevent.Message{Type: "self_log", Decoded: p}}
	}
	return protocol.Live{Event: liveevent.Message{Type: "log", Decoded: &liveevent.LogPayload{Row: p}}}
}

func TestStoredLiveHiddenPauseSelectionAndBound(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*model)
	if !strings.Contains(m.View(), "Loading stored") {
		t.Fatal(m.View())
	}
	one, two := logRow("first", 100), logRow("second", 200)
	snapshot(m, env.Owner, "list", two, one, one)
	pressKey(m, "down")
	selected := m.selected
	m.Update(protocol.Focus{Active: false}) // Hidden tabs still ingest live state.
	m.Update(event(two, false))
	m.Update(event(logRow("third", 300), true))
	if len(m.rows) != 3 || m.selected != selected {
		t.Fatal("duplicate rows or selection moved")
	}
	pressKey(m, "p")
	before := m.View()
	m.Update(event(logRow("fourth", 400), false))
	if strings.Contains(m.View(), "fourth") || !strings.Contains(before, "PAUSED") {
		t.Fatal("pause changed presentation")
	}
	pressKey(m, "p")
	if !strings.Contains(m.View(), "fourth") || m.selected != selected {
		t.Fatal("resume lost data or selection")
	}
	for n := 0; n < 150; n++ {
		m.Update(event(logRow("burst", int64(1000+n)), false))
	}
	if len(m.rows) != pageSize {
		t.Fatalf("unbounded live state: %d", len(m.rows))
	}
	// An authoritative reconnect snapshot repairs gaps and removals, without
	// preserving rows that storage no longer contains.
	m.Update(protocol.Connection{Connected: false})
	if m.connected || len(m.rows) != pageSize {
		t.Fatal("disconnect lost retained rows")
	}
	m.Update(protocol.Connection{Connected: true, Resync: true})
	snapshot(m, env.Owner, "list", one, one)
	if len(m.rows) != 1 || strings.Contains(strings.ToLower(m.View()), "resyncing") {
		t.Fatal("snapshot did not repair cache")
	}
}

func TestFiltersPagingImmutableFetchAndSupersededResults(t *testing.T) {
	queries := make(chan url.Values, 8)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		total := 201
		json.NewEncoder(w).Encode(client.Envelope[[]apigen.Log]{Data: []apigen.Log{logRow("initial API data", 100)}, Meta: &apigen.Meta{Total: &total}})
	}))
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	env := testEnv(t)
	env.API, _ = client.New(srv.URL, client.Options{})
	env.Refresh = client.NewRefreshes(env.Context, env.API)
	env.Refresh.SetSession("session")
	env.Filters.Service = "checkout"
	m := New(env).(*model)
	work := env.Refresh.Due(time.Now())
	if len(work) != 1 {
		t.Fatal(work)
	}
	result := work[0].Run()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	q := <-queries
	if q.Get("service") != "checkout" || q.Get("sessionId") != "session" || q.Get("limit") != "100" || q.Get("page") != "1" {
		t.Fatal(q)
	}
	if !env.Refresh.Complete(result, time.Now()) {
		t.Fatal("initial result rejected")
	}
	m.Update(result)
	if !strings.Contains(m.View(), "initial API data") {
		t.Fatal(m.View())
	}
	pressKey(m, "]")
	work = env.Refresh.Due(time.Now())
	if len(work) != 1 {
		t.Fatal("page not registered")
	}
	// A response already in flight must not apply after the filter changes.
	result = work[0].Run()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	q = <-queries
	pressKey(m, "s")
	pressKey(m, "ctrl+u")
	m.input = "payments"
	pressKey(m, "enter")
	if q.Get("page") != "2" || q.Get("service") != "checkout" {
		t.Fatal("worker read mutable UI state", q)
	}
	if env.Refresh.Complete(result, time.Now()) {
		t.Fatal("superseded page result accepted")
	}
	work = env.Refresh.Due(time.Now())
	result = work[0].Run()
	q = <-queries
	if q.Get("page") != "1" || q.Get("service") != "payments" {
		t.Fatal(q)
	}
	pressKey(m, "v")
	work = env.Refresh.Due(time.Now())
	work[0].Run()
	q = <-queries
	if q.Get("severity") != "trace" {
		t.Fatal(q)
	}
}

func TestFiltersDedupeAndInputCapture(t *testing.T) {
	env := testEnv(t)
	env.Filters.ErrorsOnly = true
	m := New(env).(*model)
	row := logRow("payment failed", 100)
	other := row
	other.ReceivedAt++
	info := logRow("payment info", 200)
	info.Severity = 9
	wrong := row
	wrong.SessionId = "other"
	snapshot(m, env.Owner, "list", row, info, wrong, other)
	if len(m.visible()) != 2 {
		t.Fatal("error/session filters not applied")
	}
	pressKey(m, "/")
	if !m.CapturesKeys() {
		t.Fatal("search does not capture shell keys")
	}
	pressKey(m, "q")
	pressKey(m, "esc")
	if m.text != "" || m.CapturesKeys() {
		t.Fatal("cancel did not release input")
	}
	pressKey(m, "/")
	pressKey(m, "declined")
	pressKey(m, "enter") // The server now searches attributes before pagination.
	if !m.loading || len(m.rows) != 0 {
		t.Fatal("global filter retained an unfiltered page")
	}
	snapshot(m, env.Owner, "list", row, other)
	if len(m.visible()) != 2 {
		t.Fatal("attribute search missed matches")
	}
	m.Update(event(wrong, false))
	if len(m.rows) != 2 {
		t.Fatal("wrong session live row inserted")
	}
	m.Update(protocol.SessionChanged{ID: "new"})
	if len(m.rows) != 0 || m.selected != "" || m.total != 0 {
		t.Fatal("session transition retained old rows")
	}
}

func TestFullLogNavigationMissingCorrelationAndExpiry(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "0")
	env := testEnv(t)
	m := New(env).(*model)
	row := logRow("first line\nsecond line", 100)
	snapshot(m, env.Owner, "list", row)
	nav := pressKey(m, "enter")().(protocol.ShowDetail)
	if nav.Return.Selection != identity(row) {
		t.Fatal("missing stable return context")
	}
	de := env
	de.Owner = "detail:1"
	d := nav.New(de).(*detail)
	d.Update(protocol.Connection{Connected: true})
	d.Update(protocol.SessionChanged{ID: "session"})
	d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	for _, value := range []string{"first line", "second line", "cart.items", "Received at │", row.TraceId, row.SpanId} {
		if !strings.Contains(d.View(), value) {
			t.Fatalf("full log missing %q: %s", value, d.View())
		}
	}
	trace := pressKey(d, "enter")().(protocol.OpenTrace)
	if trace.TraceID != row.TraceId || trace.SpanID != row.SpanId || trace.Return.Selection != identity(row) {
		t.Fatal(trace)
	}
	span := pressKey(d, "s")().(protocol.OpenSpan)
	if span.SpanID != row.SpanId {
		t.Fatal(span)
	}
	// Returning from an expired shared viewer leaves the full log intact.
	d.Update(trace.Return)
	if !strings.Contains(d.View(), "first line") {
		t.Fatal("Back lost log context")
	}
	snapshot(d, de.Owner, "log")
	if !strings.Contains(d.View(), "expired or removed") || !strings.Contains(d.View(), "first line") {
		t.Fatal(d.View())
	}
	snapshot(m, env.Owner, "list", logRow("new arrival", 200), row)
	m.Update(nav.Return)
	if m.selected != identity(row) {
		t.Fatal("Back did not restore selection")
	}
	uncorrelated := row
	uncorrelated.TraceId, uncorrelated.SpanId = "", ""
	d = newDetail(de, uncorrelated, de.Refresh.Session())
	if pressKey(d, "enter") != nil || !strings.Contains(d.View(), "No trace/span correlation") {
		t.Fatal(d.View())
	}
	d.Update(protocol.SessionChanged{ID: "other"})
	if d.row.Body != "" || pressKey(d, "s") != nil || !strings.Contains(d.View(), "Session changed") {
		t.Fatal("old session detail retained data")
	}
}

func TestProjectedLogHydratesOnlyUniqueDurableMatch(t *testing.T) {
	env := testEnv(t)
	stored := logRow("query result", 1793631000000000001)
	projected := stored
	projected.Attributes, projected.ReceivedAt = "", 0
	fields := []string{"timestamp_ns", "body", "service_name", "trace_id", "span_id", "severity"}
	inspector := NewQueryDetail(env, projected, "session", fields).(*detail)
	snapshot(inspector, env.Owner, "log", stored)
	if inspector.row.ReceivedAt != stored.ReceivedAt || inspector.row.Attributes != stored.Attributes || inspector.projection != nil {
		t.Fatal("unique projection did not hydrate full log")
	}
	other := stored
	other.ReceivedAt++
	ambiguous := NewQueryDetail(env, projected, "session", fields).(*detail)
	snapshot(ambiguous, env.Owner, "log", stored, other)
	if !ambiguous.ambiguous || ambiguous.row.ReceivedAt != 0 || ambiguous.missing {
		t.Fatal("ambiguous projection picked a fake identity")
	}
	if !strings.Contains(ambiguous.View(), "multiple stored logs") {
		t.Fatal(ambiguous.View())
	}
	wrong := stored
	wrong.SpanId = "unrelated"
	notFound := NewQueryDetail(env, projected, "session", fields).(*detail)
	snapshot(notFound, env.Owner, "log", wrong)
	if notFound.row.ReceivedAt != 0 || !notFound.uncertain || notFound.missing {
		t.Fatal("projection crossed correlation or reported confirmed expiry")
	}
	for _, limitSized := range []bool{false, true} {
		bounded := NewQueryDetail(env, projected, "session", fields).(*detail)
		rows := []apigen.Log{stored}
		var meta *apigen.Meta
		if limitSized {
			for i := 1; i < 1000; i++ {
				unrelated := stored
				unrelated.Body = "different"
				unrelated.ReceivedAt += int64(i)
				rows = append(rows, unrelated)
			}
		} else {
			total := 2
			meta = &apigen.Meta{Total: &total}
		}
		bounded.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: "log"}, Data: client.Envelope[[]apigen.Log]{Data: rows, Meta: meta}})
		if bounded.row.ReceivedAt != 0 || bounded.row.Attributes != "" || bounded.projection == nil || !bounded.uncertain || bounded.missing {
			t.Fatal("incomplete lookup claimed a unique identity", limitSized, bounded.row)
		}
	}
}

func TestErrorRemovalLookupLimitAndResize(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*model)
	row := logRow(strings.Repeat("long 界 body\n", 100), 100)
	snapshot(m, env.Owner, "list", row)
	m.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: "list"}, Err: errors.New("HTTP unavailable")})
	if !strings.Contains(m.View(), "HTTP unavailable") || len(m.rows) != 1 {
		t.Fatal("failure lost rows or error")
	}
	d := newDetail(env, row, env.Refresh.Session())
	total := 1001
	d.Update(protocol.Connection{Connected: true})
	d.Update(protocol.Result{Key: client.Key{Owner: env.Owner, Name: "log"}, Data: client.Envelope[[]apigen.Log]{Meta: &apigen.Meta{Total: &total}}})
	if !strings.Contains(d.View(), "bounded timestamp lookup") {
		t.Fatal("bounded lookup claimed expiry")
	}
	for _, width := range []int{1, 12, 40, 80, 120} {
		for _, view := range []tea.Model{m, d} {
			view.Update(tea.WindowSizeMsg{Width: width, Height: 12})
			text := view.View()
			if len(strings.Split(text, "\n")) > 12 {
				t.Fatal("view exceeds terminal height")
			}
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("view exceeds width %d: %q", width, line)
				}
			}
		}
	}
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressKey(d, "end")
	if d.view.YOffset == 0 {
		t.Fatal("long body cannot scroll")
	}
	if got := clean("\x1b[31mred\x1b[0m\x1b]52;c;secret\a\r\x00"); got != "red" {
		t.Fatalf("terminal escapes not removed: %q", got)
	}
}

func TestQueuedInspectorCannotReopenPreviousSession(t *testing.T) {
	for _, next := range []string{"other", ""} {
		t.Run("next="+next, func(t *testing.T) {
			env := testEnv(t)
			m := New(env).(*model)
			row := logRow("private previous session", 100)
			snapshot(m, env.Owner, "list", row)
			queued := pressKey(m, "enter")().(protocol.ShowDetail)
			env.Refresh.SetSession(next)
			env.Owner = "detail:late"
			d := queued.New(env).(*detail)
			d.Update(protocol.Connection{Connected: true})
			d.Update(protocol.SessionChanged{ID: next})
			snapshot(d, env.Owner, "log", row)
			if !d.changedSession || d.row.Body != "" || strings.Contains(d.View(), row.Body) || pressKey(d, "enter") != nil {
				t.Fatal("queued navigation reopened previous-session data")
			}
		})
	}
}
