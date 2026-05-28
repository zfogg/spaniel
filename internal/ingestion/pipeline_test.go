package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/gorilla/websocket"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// dialPipelineHub dials the hub's WS endpoint and returns a client conn.
func dialPipelineHub(t *testing.T, h *ws.Hub) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial ws: %v", err)
	}
	return conn, srv
}

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// makeTraces builds a ptrace.Traces with one ResourceSpans (carrying service.name)
// and the given child spans. If makeSpan returns kind=0, default Kind unset.
func makeTraces(serviceName string, makeSpans func(ss ptrace.ScopeSpans)) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", serviceName)
	rs.Resource().Attributes().PutStr("deployment.environment", "test")
	ss := rs.ScopeSpans().AppendEmpty()
	makeSpans(ss)
	return td
}

func putSpan(ss ptrace.ScopeSpans, traceID, spanID, parent, name string, kind ptrace.SpanKind, attrs map[string]string) {
	s := ss.Spans().AppendEmpty()
	var tid pcommon.TraceID
	var sid pcommon.SpanID
	copy(tid[:], []byte(strings.Repeat(traceID, 16))[:16])
	copy(sid[:], []byte(strings.Repeat(spanID, 8))[:8])
	s.SetTraceID(tid)
	s.SetSpanID(sid)
	if parent != "" {
		var pid pcommon.SpanID
		copy(pid[:], []byte(strings.Repeat(parent, 8))[:8])
		s.SetParentSpanID(pid)
	}
	s.SetName(name)
	s.SetKind(kind)
	s.SetStartTimestamp(pcommon.Timestamp(1_000_000_000))
	s.SetEndTimestamp(pcommon.Timestamp(1_500_000_000))
	for k, v := range attrs {
		s.Attributes().PutStr(k, v)
	}
}

func TestPipelineIngestTraces_NormalizationAndSessionTag(t *testing.T) {
	db := openTestDB(t)
	sess, err := db.CreateSession("test-session", false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("checkout-api", func(ss ptrace.ScopeSpans) {
		putSpan(ss, "a", "1", "", "GET /cart", ptrace.SpanKindServer, map[string]string{
			"http.request.method":       "GET",
			"http.response.status_code": "200",
		})
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	traceIDStr := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()
	spans, err := db.GetTrace(traceIDStr)
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	got := spans[0]
	if got.ServiceName != "checkout-api" {
		t.Errorf("service.name not extracted from resource: got %q", got.ServiceName)
	}
	if got.SessionID != sess.ID || got.SessionLabel != sess.Label {
		t.Errorf("session not tagged: got id=%q label=%q", got.SessionID, got.SessionLabel)
	}
	if !strings.Contains(got.Resource, "deployment.environment") {
		t.Errorf("resource attrs not flattened: %q", got.Resource)
	}
	if got.StartNs != 1_000_000_000 || got.EndNs != 1_500_000_000 {
		t.Errorf("ns timestamps not preserved: start=%d end=%d", got.StartNs, got.EndNs)
	}
}

func TestPipelineIngestTraces_LintWarningsCreated(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("lint-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("svc", func(ss ptrace.ScopeSpans) {
		// HTTP client span missing both method and status_code → should fire two warnings.
		putSpan(ss, "b", "2", "", "outgoing", ptrace.SpanKindClient, map[string]string{})
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	// lintSpan runs in a goroutine — poll briefly.
	var warnings []*storage.LintWarning
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		warnings, _ = db.ListLintWarnings(sess.ID)
		if len(warnings) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(warnings) < 2 {
		t.Fatalf("expected lint warnings to be created async, got %d", len(warnings))
	}
	ruleIDs := map[string]bool{}
	for _, w := range warnings {
		ruleIDs[w.RuleID] = true
	}
	if !ruleIDs["http.missing_method"] || !ruleIDs["http.missing_status_code"] {
		t.Errorf("expected http.missing_method and http.missing_status_code, got %v", ruleIDs)
	}
}

func TestPipelineIngestTraces_DetectorDebouncedN1(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("n1-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("svc", func(ss ptrace.ScopeSpans) {
		// Parent span "p" then 12 repeated DB statements under it → N+1 candidate.
		putSpan(ss, "c", "p", "", "GET /users", ptrace.SpanKindServer, map[string]string{})
		for i := range 12 {
			putSpan(ss, "c", string(rune('a'+i)), "p", "db.query", ptrace.SpanKindClient, map[string]string{
				"db.system":    "postgresql",
				"db.statement": "SELECT * FROM users WHERE id = " + string(rune('0'+i%10)),
			})
		}
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	traceIDStr := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID().String()

	// Detector fires 500ms after the last span; poll until it lands.
	var issues []*storage.TraceIssue
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		issues, _ = db.GetTraceIssues(traceIDStr)
		if len(issues) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(issues) == 0 {
		t.Fatalf("expected debounced N+1 detector to upsert a trace issue")
	}
	if issues[0].Kind != "n_plus_one" {
		t.Errorf("expected kind=n_plus_one, got %q", issues[0].Kind)
	}
	if issues[0].Count < 10 {
		t.Errorf("expected count >= 10, got %d", issues[0].Count)
	}
}

func TestPipelineScheduleDetectors_DebouncesRapidArrivals(t *testing.T) {
	db := openTestDB(t)
	p := NewPipeline(db, ws.NewHub())

	// Fire scheduleDetectors many times rapidly for the same trace; expect
	// a single pending timer (subsequent calls reset the timer rather than queue).
	for range 50 {
		p.scheduleDetectors("trace-x")
	}
	p.debounceMu.Lock()
	count := len(p.debounce)
	p.debounceMu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 debounced timer for trace, got %d", count)
	}
}

func TestPipelineIngestTraces_SpanEventsPersisted(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("events-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	td := makeTraces("svc", func(ss ptrace.ScopeSpans) {
		s := ss.Spans().AppendEmpty()
		var tid pcommon.TraceID
		var sid pcommon.SpanID
		copy(tid[:], []byte(strings.Repeat("e", 16))[:16])
		copy(sid[:], []byte(strings.Repeat("E", 8))[:8])
		s.SetTraceID(tid)
		s.SetSpanID(sid)
		s.SetName("GET /things")
		s.SetKind(ptrace.SpanKindServer)
		s.SetStartTimestamp(pcommon.Timestamp(1_000_000_000))
		s.SetEndTimestamp(pcommon.Timestamp(1_500_000_000))

		// Three events on this span — one plain, one with an attribute,
		// and one canonical OTel "exception" event.
		e1 := s.Events().AppendEmpty()
		e1.SetTimestamp(pcommon.Timestamp(1_000_400_000))
		e1.SetName("pg.connection.acquired")

		e2 := s.Events().AppendEmpty()
		e2.SetTimestamp(pcommon.Timestamp(1_006_100_000))
		e2.SetName("pg.row.fetched")
		e2.Attributes().PutInt("rows", 1)

		e3 := s.Events().AppendEmpty()
		e3.SetTimestamp(pcommon.Timestamp(1_007_800_000))
		e3.SetName("exception")
		e3.Attributes().PutStr("exception.type", "DBError")
		e3.Attributes().PutStr("exception.message", "deadlock detected")
		e3.Attributes().PutStr("exception.stacktrace", "at L1\nat L2")
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	spanIDStr := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).SpanID().String()
	events, err := db.ListEventsBySpan(spanIDStr)
	if err != nil {
		t.Fatalf("ListEventsBySpan: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	if events[0].Name != "pg.connection.acquired" || events[0].TimeNs != 1_000_400_000 {
		t.Errorf("event[0] wrong: %+v", events[0])
	}
	if events[1].Name != "pg.row.fetched" || !strings.Contains(events[1].Attributes, `"rows":1`) {
		t.Errorf("event[1] wrong: %+v", events[1])
	}
	if events[2].Name != "exception" || !strings.Contains(events[2].Attributes, "exception.stacktrace") {
		t.Errorf("event[2] (exception) wrong: %+v", events[2])
	}
}

func TestPipelineIngestTraces_SpanLinksPersisted(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("links-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	p := NewPipeline(db, ws.NewHub())
	// Two link targets: (linkedTrace1, linkedSpan1) + (linkedTrace2, linkedSpan2).
	td := makeTraces("svc", func(ss ptrace.ScopeSpans) {
		s := ss.Spans().AppendEmpty()
		var tid pcommon.TraceID
		var sid pcommon.SpanID
		copy(tid[:], []byte(strings.Repeat("l", 16))[:16])
		copy(sid[:], []byte(strings.Repeat("L", 8))[:8])
		s.SetTraceID(tid)
		s.SetSpanID(sid)
		s.SetName("kafka.consume")
		s.SetKind(ptrace.SpanKindConsumer)
		s.SetStartTimestamp(pcommon.Timestamp(1_000_000_000))
		s.SetEndTimestamp(pcommon.Timestamp(1_500_000_000))

		// Link #1 — references a producer span in another trace.
		l1 := s.Links().AppendEmpty()
		var lt1 pcommon.TraceID
		var ls1 pcommon.SpanID
		copy(lt1[:], []byte(strings.Repeat("p", 16))[:16])
		copy(ls1[:], []byte(strings.Repeat("P", 8))[:8])
		l1.SetTraceID(lt1)
		l1.SetSpanID(ls1)
		l1.TraceState().FromRaw("rojo=00f067aa0ba902b7")
		l1.Attributes().PutStr("messaging.operation", "process")

		// Link #2 — references a different producer.
		l2 := s.Links().AppendEmpty()
		var lt2 pcommon.TraceID
		var ls2 pcommon.SpanID
		copy(lt2[:], []byte(strings.Repeat("q", 16))[:16])
		copy(ls2[:], []byte(strings.Repeat("Q", 8))[:8])
		l2.SetTraceID(lt2)
		l2.SetSpanID(ls2)
	})
	if err := p.IngestTraces(context.Background(), td); err != nil {
		t.Fatalf("IngestTraces: %v", err)
	}

	spanIDStr := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).SpanID().String()
	links, err := db.ListLinksBySpan(spanIDStr)
	if err != nil {
		t.Fatalf("ListLinksBySpan: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("want 2 links, got %d", len(links))
	}
	// Order in DB is insertion order; both targets should be present.
	gotTargets := map[string]string{}
	for _, l := range links {
		gotTargets[l.LinkedTraceID] = l.LinkedSpanID
	}
	if len(gotTargets) != 2 {
		t.Errorf("expected 2 distinct linked trace IDs, got %+v", gotTargets)
	}
	// Trace state + attribute round-trip on link #1.
	var first *storage.SpanLink
	for _, l := range links {
		if strings.Contains(l.TraceState, "rojo") {
			first = l
		}
	}
	if first == nil {
		t.Fatal("expected at least one link to carry the trace_state")
	}
	if !strings.Contains(first.Attributes, `"messaging.operation":"process"`) {
		t.Errorf("link attributes lost: %q", first.Attributes)
	}

	// Reverse lookup: ListIncomingLinks for one of the linked traces returns
	// this span as the caller.
	rev, err := db.ListIncomingLinks(first.LinkedTraceID)
	if err != nil {
		t.Fatalf("ListIncomingLinks: %v", err)
	}
	if len(rev) != 1 || rev[0].SpanID != spanIDStr {
		t.Errorf("incoming-link reverse lookup wrong: %+v", rev)
	}
}

func makeLogs(serviceName string, makeFn func(rl plog.ResourceLogs)) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", serviceName)
	makeFn(rl)
	return ld
}

func makeMetrics(serviceName string, makeFn func(rm pmetric.ResourceMetrics)) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", serviceName)
	makeFn(rm)
	return md
}

func TestPipeline_BroadcastsLog(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("log-ws-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	hub := ws.NewHub()
	conn, srv := dialPipelineHub(t, hub)
	defer srv.Close()
	defer conn.Close()

	p := NewPipeline(db, hub)

	ld := makeLogs("log-svc", func(rl plog.ResourceLogs) {
		sl := rl.ScopeLogs().AppendEmpty()
		lr := sl.LogRecords().AppendEmpty()
		lr.SetSeverityNumber(plog.SeverityNumberInfo)
		lr.Body().SetStr("hello log")
	})

	if err := p.IngestLogs(context.Background(), ld); err != nil {
		t.Fatalf("IngestLogs: %v", err)
	}

	// Read frames until we find a "log" event (span broadcasts may arrive first)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read message: %v", err)
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Body string `json:"body"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.Type != "log" {
			continue
		}
		if ev.Payload.Body != "hello log" {
			t.Errorf("expected body=%q, got %q", "hello log", ev.Payload.Body)
		}
		return
	}
}

func TestPipeline_BroadcastsMetric(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("metric-ws-session", false)
	db.SetActiveSession(sess.ID, sess.Label)

	hub := ws.NewHub()
	conn, srv := dialPipelineHub(t, hub)
	defer srv.Close()
	defer conn.Close()

	p := NewPipeline(db, hub)

	md := makeMetrics("metric-svc", func(rm pmetric.ResourceMetrics) {
		sm := rm.ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("http.requests")
		g := m.SetEmptyGauge()
		dp := g.DataPoints().AppendEmpty()
		dp.SetDoubleValue(42.0)
		dp.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	})

	if err := p.IngestMetrics(context.Background(), md); err != nil {
		t.Fatalf("IngestMetrics: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read message: %v", err)
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Name string `json:"name"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.Type != "metric" {
			continue
		}
		if ev.Payload.Name != "http.requests" {
			t.Errorf("expected name=%q, got %q", "http.requests", ev.Payload.Name)
		}
		return
	}
}
