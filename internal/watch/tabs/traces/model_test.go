package traces

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
)

func testEnv(owner string) protocol.Env {
	ctx := context.Background()
	r := client.NewRefreshes(ctx, nil)
	r.SetSession("seed")
	return protocol.Env{Context: ctx, Refresh: r, Owner: owner}
}
func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
func result(owner, name string, seq uint64, data any) protocol.Result {
	return protocol.Result{Key: client.Key{Owner: owner, Name: name}, Session: "seed", Sequence: seq, Data: data}
}
func sampleSpan(id, parent string, start int64) apigen.Span {
	return apigen.Span{SessionId: "seed", TraceId: "trace", SpanId: id, ParentSpanId: parent, Name: id, ServiceName: "api", StartNs: start, EndNs: start + 100, DurationNs: 100, Attributes: `{"http.route":"/orders"}`, Resource: `{"service.version":"1.2"}`}
}
func spanEvent(s apigen.Span) protocol.Live {
	p := convert[liveevent.SelfSpanPayload](s)
	return protocol.Live{Event: liveevent.Message{Type: "self_span", Decoded: &p}}
}

func TestListsSelectionFiltersPagingPauseAndLiveRows(t *testing.T) {
	for _, spans := range []bool{false, true} {
		t.Run(fmt.Sprint(spans), func(t *testing.T) {
			m := newList(testEnv("list-owner"), spans)
			a, b := sampleSpan("a", "", 1000), sampleSpan("b", "a", 1100)
			b.StatusCode = 2
			if spans {
				m.Update(result(m.env.Owner, "list", 1, client.Envelope[[]apigen.SpanRow]{Data: []apigen.SpanRow{spanRow(a), spanRow(b)}}))
			} else {
				m.Update(result(m.env.Owner, "list", 1, client.Envelope[[]apigen.TraceRow]{Data: []apigen.TraceRow{{SessionId: "seed", TraceId: "a", Name: "a", StartNs: 1000}, {SessionId: "seed", TraceId: "b", Name: "b", StartNs: 1100, StatusCode: 2, HasN1: true}}}))
			}
			m.Update(press("down"))
			selected := m.selected
			m.Update(press("p"))
			if spans {
				c := sampleSpan("c", "", 1200)
				m.Update(spanEvent(c))
				m.Update(spanEvent(c))
			} else {
				p := liveevent.SelfTracePayload{SessionID: "seed", TraceID: "c", Name: "c", StartNs: 1200}
				e := protocol.Live{Event: liveevent.Message{Type: "self_trace", Decoded: &p}}
				m.Update(e)
				m.Update(e)
			}
			if len(m.visible()) != 2 || len(m.rows) != 3 || m.selected != selected {
				t.Fatal("pause lost presentation, deduplication or selection")
			}
			m.Update(press("p"))
			if len(m.visible()) != 3 || m.selected != selected {
				t.Fatal("unpause did not reconcile stable selection")
			}
			m.Update(press("e"))
			if len(m.visible()) != 1 || m.visible()[0].status != 2 {
				t.Fatal("error filter did not operate on status")
			}
			_, cmd := m.Update(press("enter"))
			if cmd == nil {
				t.Fatal("no selected-row navigation")
			}
			if spans {
				if _, ok := cmd().(protocol.OpenSpan); !ok {
					t.Fatal("span tab did not open inspector")
				}
			} else {
				if _, ok := cmd().(protocol.OpenTrace); !ok {
					t.Fatal("trace tab did not open waterfall")
				}
			}
			m.Update(press("/"))
			m.Update(press("q"))
			if !m.CapturesKeys() {
				t.Fatal("filter must capture global keys")
			}
			m.Update(press("esc"))
			if m.text != "" {
				t.Fatal("cancel applied filter")
			}
			m.Update(press("/"))
			m.Update(press("b"))
			m.Update(press("enter"))
			if len(m.visible()) != 1 {
				t.Fatal("text search lost matching row")
			}
			m.total = 201
			m.Update(press("]"))
			if m.page != 2 || len(m.rows) != 0 {
				t.Fatal("page did not replace bounded snapshot")
			}
			m.Update(spanEvent(sampleSpan("off-page", "", 1300)))
			if len(m.rows) != 0 {
				t.Fatal("live insertion corrupted later page")
			}
			m.Update(protocol.SessionChanged{ID: "new"})
			if m.page != 1 || len(m.rows) != 0 || m.selected != "" {
				t.Fatal("session retained old page")
			}
			m.Update(result(m.env.Owner, "list", 99, client.Envelope[[]apigen.SpanRow]{Data: []apigen.SpanRow{spanRow(a)}}))
			if len(m.rows) != 0 {
				t.Fatal("old-session result escaped guard")
			}
		})
	}
}

func TestTraceSpanRoundTripAndLateParentSelection(t *testing.T) {
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace", SpanID: "child"}).(*traceModel)
	child := sampleSpan("child", "parent", 2000)
	child.StatusCode = 2
	m.Update(result(m.env.Owner, "trace", 1, client.Envelope[[]apigen.Span]{Data: []apigen.Span{child, child}}))
	if len(m.rows()) != 1 || !m.rows()[0].orphan || m.selected != "seed/child" {
		t.Fatal("missing-parent selection failed")
	}
	m.Update(protocol.Focus{Active: false})
	m.Update(spanEvent(sampleSpan("parent", "", 1000)))
	m.Update(spanEvent(sampleSpan("late-sibling", "parent", 1500)))
	if len(m.rows()) != 3 || m.rows()[2].depth != 1 || m.selected != "seed/child" {
		t.Fatal("hidden late-span update moved selection")
	}
	_, cmd := m.Update(press("enter"))
	target, ok := cmd().(protocol.OpenSpan)
	if !ok || target.SpanID != "child" || target.Return.Selection != "seed/child" {
		t.Fatal("trace to inspector lost return context")
	}
	inspector := NewDetails().Span(testEnv("detail:2"), target).(*spanModel)
	inspector.Update(result(inspector.env.Owner, "span", 2, client.Envelope[apigen.Span]{Data: child}))
	_, cmd = inspector.Update(press("t"))
	trace, ok := cmd().(protocol.OpenTrace)
	if !ok || trace.TraceID != "trace" || trace.SpanID != "child" {
		t.Fatal("inspector to containing trace lost selected span")
	}
	m.Update(protocol.ReturnContext{Selection: target.Return.Selection})
	if m.selected != "seed/child" {
		t.Fatal("Back lost originating waterfall selection")
	}
	// Authoritative deletion clears prior rows even after a live patch.
	m.Update(result(m.env.Owner, "trace", 3, client.Envelope[[]apigen.Span]{}))
	if len(m.rows()) != 0 || !strings.Contains(m.View(), "expired") {
		t.Fatal("expired trace remained current")
	}
}

func TestInspectorCompleteDataRelatedLogsAndErrors(t *testing.T) {
	m := NewDetails().Span(testEnv("detail:1"), protocol.OpenSpan{TraceID: "trace", SpanID: "child"}).(*spanModel)
	s := sampleSpan("child", "parent", 1793631000000000000)
	s.StatusCode = 2
	s.StatusMessage = "database timeout"
	// Decode the actual API event/link shapes, including duplicate delivery.
	s = convert[apigen.Span](map[string]any{"session_id": s.SessionId, "trace_id": s.TraceId, "span_id": s.SpanId, "parent_span_id": s.ParentSpanId, "start_ns": s.StartNs, "end_ns": s.EndNs, "duration_ns": s.DurationNs, "status_code": 2, "status_message": s.StatusMessage, "attributes": s.Attributes, "resource": s.Resource,
		"events": []any{map[string]any{"name": "exception", "time_ns": s.StartNs, "attributes": `{"exception.message":"timeout"}`}, map[string]any{"name": "exception", "time_ns": s.StartNs, "attributes": `{"exception.message":"timeout"}`}},
		"links":  []any{map[string]any{"linked_trace_id": "linked-trace", "linked_span_id": "linked-span", "trace_state": "vendor=state", "attributes": "{}"}}})
	m.Update(result(m.env.Owner, "span", 1, client.Envelope[apigen.Span]{Data: s}))
	l := apigen.Log{SessionId: "seed", TraceId: "trace", SpanId: "child", Body: "query failed", TimestampNs: s.StartNs, ReceivedAt: 1}
	m.Update(result(m.env.Owner, "logs", 2, client.Envelope[[]apigen.Log]{Data: []apigen.Log{l, l, {SessionId: "seed", TraceId: "other", SpanId: "child", Body: "wrong trace"}}}))
	content := ansi.Strip(strings.Join(m.content(), "\n"))
	for _, text := range []string{"database timeout", "Trace ID  │ trace", "Span ID   │ child", "Parent ID │ parent", "http.route", "service.version", "exception.message", "linked-trace", "linked-span", "vendor=state", "2026-11-02 14:50:00.000 UTC", "query failed"} {
		if !strings.Contains(content, text) {
			t.Fatalf("inspector omitted %q", text)
		}
	}
	if len(m.logs) != 1 || strings.Count(content, "exception.message") != 1 || strings.Contains(content, "wrong trace") {
		t.Fatal("related rows were duplicated or cross-correlated")
	}
	p := convert[liveevent.SelfLogPayload](l)
	event := protocol.Live{Event: liveevent.Message{Type: "self_log", Decoded: &p}}
	m.Update(event)
	m.Update(event)
	if len(m.logs) != 1 {
		t.Fatal("live log replay duplicated row")
	}
	p.Body = "late related log"
	p.ReceivedAt = 2
	m.Update(event)
	if len(m.logs) != 2 {
		t.Fatal("late correlated log not updated")
	}
	s.StatusMessage = "recovered"
	s.StatusCode = 1
	s.Events = nil
	s.Links = nil
	m.Update(spanEvent(s))
	if m.span.StatusMessage != "recovered" || len(m.span.Events) == 0 || len(m.span.Links) == 0 {
		t.Fatal("live span update missed hidden inspector")
	}
	errResult := result(m.env.Owner, "span", 3, nil)
	errResult.Err = errors.New("offline")
	m.Update(errResult)
	if m.span == nil || !strings.Contains(m.View(), "Error:") {
		t.Fatal("transport error discarded useful stale span")
	}
	errResult.Sequence = 4
	errResult.Err = &client.HTTPError{Status: 404, Message: "span expired"}
	m.Update(errResult)
	if m.span != nil {
		t.Fatal("404 retained deleted span")
	}
	m.Update(protocol.SessionChanged{ID: "other"})
	if len(m.logs) != 0 || m.span != nil {
		t.Fatal("session leaked old detail")
	}
}

func TestTimingHierarchyAndNarrowScreens(t *testing.T) {
	spans := []apigen.Span{sampleSpan("root", "", 1000), sampleSpan("child", "root", 1200), sampleSpan("missing", "no-parent", 1300), sampleSpan("cycle-a", "cycle-b", 1400), sampleSpan("cycle-b", "cycle-a", 1500), sampleSpan("self", "self", 1600)}
	for _, s := range []apigen.Span{{SpanId: "no-time", SessionId: "seed"}, {SpanId: "negative", StartNs: 1000, EndNs: 900, DurationNs: -100}, {SpanId: "overflow", StartNs: math.MaxInt64 - 1, DurationNs: 100}} {
		spans = append(spans, s)
	}
	rows := flatten(spans)
	childDepth := -1
	for _, row := range rows {
		if row.span.SpanId == "child" {
			childDepth = row.depth
		}
	}
	if len(rows) != len(spans) || childDepth != 1 {
		t.Fatal("invalid hierarchy omitted spans")
	}
	start, end := traceWindow(rows)
	for _, r := range rows {
		for _, w := range []int{1, 2, 10, 60, 120} {
			bar := waterfallBar(r.span, start, end, w)
			if ansi.StringWidth(bar) != w {
				t.Fatalf("bar width %d: %q", w, bar)
			}
		}
	}
	if waterfallBar(sampleSpan("offset", "", 1500), 1000, 2000, 10) != "     █    " {
		t.Fatal("bar was not time aligned")
	}
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace"}).(*traceModel)
	m.spans = spans
	m.selectExisting()
	i := NewDetails().Span(testEnv("detail:2"), protocol.OpenSpan{SpanID: "root"}).(*spanModel)
	i.span = &spans[0]
	list := newList(testEnv("traces"), false)
	for _, model := range []tea.Model{m, i, list} {
		for _, size := range []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 12, Height: 8}, {Width: 80, Height: 12}, {Width: 120, Height: 30}} {
			model.Update(size)
			model.Update(press("end"))
			view := model.View()
			if size.Height == 0 && view != "" {
				t.Fatal("zero-height view")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > max(1, size.Width) {
					t.Fatal("view overflowed narrow terminal")
				}
			}
		}
	}
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	errSpan := sampleSpan("error", "", 1000)
	errSpan.StatusCode = 2
	row := waterfallRow(flatSpan{span: errSpan}, true, 1000, 1100, 100)
	if !strings.Contains(row, "ERROR") || !strings.Contains(row, "38;2;239;107;115") {
		t.Fatalf("error span is not visibly labeled red: %q", row)
	}
	if clean("safe\x1b]0;bad\a\ntext") != "safe text" {
		t.Fatal("telemetry escaped terminal sanitization")
	}
}

func TestResultsRejectWrongOwnerSessionAndOldSequence(t *testing.T) {
	m := newList(testEnv("traces"), false)
	good := result("traces", "list", 5, client.Envelope[[]apigen.TraceRow]{Data: []apigen.TraceRow{{SessionId: "seed", TraceId: "good"}}})
	m.Update(good)
	for _, r := range []protocol.Result{result("other", "list", 6, client.Envelope[[]apigen.TraceRow]{}), result("traces", "list", 4, client.Envelope[[]apigen.TraceRow]{}), {Key: client.Key{Owner: "traces", Name: "list"}, Session: "old", Sequence: 99, Data: client.Envelope[[]apigen.TraceRow]{}}} {
		m.Update(r)
	}
	if len(m.rows) != 1 || m.rows[0].trace != "good" {
		t.Fatal("stale or foreign result replaced current list")
	}
}

func TestRequestReplacementCapturesImmutableScopedQuery(t *testing.T) {
	queries := make(chan url.Values, 4)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		fmt.Fprint(w, `{"data":[],"meta":{"total":0}}`)
	}))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	api, err := client.New(server.URL, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	env := testEnv("spans")
	env.API = api
	env.Refresh = client.NewRefreshes(env.Context, api)
	env.Refresh.SetSession("seed")
	env.Filters.Service = "old-service"
	m := newList(env, true)
	old := env.Refresh.Due(time.Now())[0]
	finished := make(chan protocol.Result, 1)
	go func() { finished <- old.Run() }()
	var q url.Values
	select {
	case q = <-queries:
	case <-time.After(5 * time.Second):
		t.Fatal("initial request did not start")
	}
	m.Update(press("s"))
	m.draft = "new-service"
	m.Update(press("enter"))
	oldResult := <-finished
	if env.Refresh.Complete(oldResult, time.Now()) {
		t.Fatal("replaced request was still authoritative")
	}
	if q.Get("service") != "old-service" || q.Get("sessionId") != "seed" || q.Get("limit") != "100" || q.Get("view") != "flat" {
		t.Fatalf("worker read mutable model query: %v", q)
	}
	work := env.Refresh.Due(time.Now())
	if len(work) != 1 {
		t.Fatal("replacement was not queued")
	}
	r := work[0].Run()
	if !env.Refresh.Complete(r, time.Now()) {
		t.Fatal("new request rejected")
	}
	m.Update(r)
	q = <-queries
	if q.Get("service") != "new-service" || q.Get("page") != "1" {
		t.Fatalf("replacement query: %v", q)
	}
	if m.state.loading || m.state.err != "" {
		t.Fatal("successful replacement did not settle")
	}
}

func TestRequestedSpanSelectsWhenItArrivesLate(t *testing.T) {
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace", SpanID: "late"}).(*traceModel)
	m.Update(result(m.env.Owner, "trace", 1, client.Envelope[[]apigen.Span]{Data: []apigen.Span{sampleSpan("root", "", 1000)}}))
	m.Update(spanEvent(sampleSpan("late", "root", 1200)))
	if m.selected != "seed/late" {
		t.Fatal("late external navigation target was lost")
	}
}

func TestSelfSpanPatchNeverRestartsRequests(t *testing.T) {
	env := testEnv("detail:1")
	m := NewDetails().Span(env, protocol.OpenSpan{SpanID: "a"}).(*spanModel)
	now := time.Now()
	for _, work := range env.Refresh.Due(now) {
		if !env.Refresh.Complete(work.Result, now) {
			t.Fatal("initial work rejected")
		}
	}
	m.Update(spanEvent(sampleSpan("a", "", 1000)))
	if m.traceID != "trace" || len(env.Refresh.Due(now.Add(2*time.Second))) != 0 {
		t.Fatal("self patch restarted HTTP work")
	}
}

func TestLongTraceScrollKeepsLastSpanVisible(t *testing.T) {
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace"}).(*traceModel)
	parent := ""
	for i := 0; i < 3000; i++ {
		id := fmt.Sprintf("operation-%04d", i)
		m.spans = append(m.spans, sampleSpan(id, parent, int64(1000+i)))
		parent = id
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m.selectExisting()
	m.Update(press("end"))
	if m.selected != "seed/operation-2999" || !strings.Contains(m.View(), "operation-2999") {
		t.Fatal("long trace did not scroll to selection")
	}
}

func TestLiveSpanPreservesDerivedFilterAndRejectsMalformedMetadata(t *testing.T) {
	m := newList(testEnv("spans"), true)
	s := sampleSpan("n1", "root", 1000)
	row := spanRow(s)
	tag := "n+1"
	row.Tag = &tag
	m.Update(result("spans", "list", 1, client.Envelope[[]apigen.SpanRow]{Data: []apigen.SpanRow{row}}))
	m.Update(press("n"))
	selected := m.selected
	m.Update(spanEvent(s))
	if len(m.visible()) != 1 || m.selected != selected {
		t.Fatal("live row erased the server-derived N+1 filter/selection")
	}
	p := convert[liveevent.SelfSpanPayload](s)
	p.Events = []any{map[string]any{"time_ns": "invalid timestamp"}}
	if _, ok := eventSpan(liveevent.Message{Type: "self_span", Decoded: &p}); ok {
		t.Fatal("partially decoded malformed span accepted")
	}
}

func TestBoundedTraceLiveWindowPinsSelectionUntilSnapshotRepair(t *testing.T) {
	m := NewDetails().Trace(testEnv("detail:1"), protocol.OpenTrace{TraceID: "trace", SpanID: "selected"}).(*traceModel)
	selected := sampleSpan("selected", "", 1000)
	m.Update(result(m.env.Owner, "trace", 1, client.Envelope[[]apigen.Span]{Data: []apigen.Span{selected}}))
	m.liveLimit = 3 // Exercise the same bound without manufacturing 20k events.
	for i := 0; i < 8; i++ {
		m.Update(spanEvent(sampleSpan(fmt.Sprint(i), "selected", int64(1100+i))))
	}
	if len(m.spans) != 3 || m.selected != "seed/selected" || !strings.Contains(m.View(), "Live window full") {
		t.Fatal("bounded window hid truncation or evicted selection")
	}
	m.Update(result(m.env.Owner, "trace", 2, client.Envelope[[]apigen.Span]{Data: []apigen.Span{selected}}))
	if m.truncated || len(m.spans) != 1 || m.selected != "seed/selected" {
		t.Fatal("authoritative repair did not clear bounded live state")
	}
}
