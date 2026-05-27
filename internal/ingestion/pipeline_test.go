package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

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
