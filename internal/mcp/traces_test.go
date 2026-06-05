package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

const (
	testTraceID = "trace-aaaaaaaa"
	rootSpanID  = "span-root"
	dbSpanID    = "span-db"
)

// seedTrace inserts a two-span trace (api root → postgres child) into sessID,
// with an exception event, an outgoing link, a correlated log, an n+1 issue,
// and a lint warning — enough to exercise every facet of get_trace.
func seedTrace(t *testing.T, store *storage.DB, sessID string) {
	t.Helper()
	root := &storage.Span{
		TraceID: testTraceID, SpanID: rootSpanID, ServiceName: "api",
		Name: "GET /api/users", Kind: 2,
		StartNs: 1_000_000, EndNs: 151_000_000, // 150ms
		StatusCode: 1,
		Attributes: `{"http.method":"GET","http.route":"/api/users"}`,
		SessionID:  sessID, SessionLabel: sessID, ReceivedAt: 1,
	}
	db := &storage.Span{
		TraceID: testTraceID, SpanID: dbSpanID, ParentSpanID: rootSpanID, ServiceName: "postgres",
		Name: "SELECT users", Kind: 3,
		StartNs: 2_000_000, EndNs: 302_000_000, // 300ms
		StatusCode: 2, StatusMessage: "boom",
		Attributes: `{"db.system":"postgresql","db.statement":"SELECT * FROM users"}`,
		SessionID:  sessID, SessionLabel: sessID, ReceivedAt: 1,
	}
	if err := store.InsertSpan(root); err != nil {
		t.Fatalf("insert root span: %v", err)
	}
	if err := store.InsertSpan(db); err != nil {
		t.Fatalf("insert db span: %v", err)
	}
	if err := store.InsertSpanEvents([]*storage.SpanEvent{{
		SpanID: dbSpanID, TraceID: testTraceID, SessionID: sessID, TimeNs: 2_500_000,
		Name: "exception", Attributes: `{"exception.message":"boom"}`,
	}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := store.InsertSpanLinks([]*storage.SpanLink{{
		SpanID: rootSpanID, TraceID: testTraceID, SessionID: sessID,
		LinkedTraceID: "other-trace", LinkedSpanID: "other-span", Attributes: "{}",
	}}); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	if err := store.InsertLog(&storage.Log{
		TimestampNs: 2_600_000, TraceID: testTraceID, SpanID: dbSpanID, Severity: 17,
		Body: "query failed", ServiceName: "postgres", SessionID: sessID, ReceivedAt: 1,
	}); err != nil {
		t.Fatalf("insert log: %v", err)
	}
	if err := store.UpsertTraceIssue(&storage.TraceIssue{
		ID: "iss-1", TraceID: testTraceID, SessionID: sessID, Kind: "n_plus_one",
		Count: 5, WastedNs: 50_000_000, ParentSpanID: rootSpanID, ExampleSpanID: dbSpanID, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("upsert issue: %v", err)
	}
	if err := store.InsertLintWarning(&storage.LintWarning{
		SpanID: dbSpanID, TraceID: testTraceID, SessionID: sessID, RuleID: "db.system_required",
		Severity: "warn", Message: "missing db.system", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("insert lint: %v", err)
	}
}

// callStructured calls a tool and decodes its structured output into T.
func callStructured[T any](t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %+v", name, res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s output: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s output: %v", name, err)
	}
	return out
}

func TestListTraces(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListTracesOutput](t, cs, "list_traces", nil)
	if out.Count != 1 {
		t.Fatalf("expected 1 trace, got %d", out.Count)
	}
	tr := out.Traces[0]
	if tr.TraceID != testTraceID {
		t.Errorf("trace_id = %q, want %q", tr.TraceID, testTraceID)
	}
	if tr.Service != "api" || tr.RootName != "GET /api/users" {
		t.Errorf("root = %s/%s, want api/GET /api/users", tr.Service, tr.RootName)
	}
	if tr.SpanCount != 2 {
		t.Errorf("span_count = %d, want 2", tr.SpanCount)
	}
	if !tr.HasN1 {
		t.Error("has_n1 = false, want true")
	}
	if len(tr.IssueKinds) != 1 || tr.IssueKinds[0] != "n_plus_one" {
		t.Errorf("issue_kinds = %v, want [n_plus_one]", tr.IssueKinds)
	}
	if tr.DurationMs != 150.0 {
		t.Errorf("duration_ms = %v, want 150", tr.DurationMs)
	}
}

func TestListTraces_ServiceFilter(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	hit := callStructured[ListTracesOutput](t, cs, "list_traces", map[string]any{"service": "api"})
	if hit.Count != 1 {
		t.Errorf("service=api: expected 1 trace, got %d", hit.Count)
	}
	miss := callStructured[ListTracesOutput](t, cs, "list_traces", map[string]any{"service": "nope"})
	if miss.Count != 0 {
		t.Errorf("service=nope: expected 0 traces, got %d", miss.Count)
	}
}

func TestGetTrace(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "get_trace", Arguments: map[string]any{"trace_id": testTraceID},
	})
	if err != nil {
		t.Fatalf("call get_trace: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_trace error: %+v", res.Content)
	}

	// Waterfall is surfaced as text content.
	if len(res.Content) == 0 {
		t.Fatal("expected text content (waterfall)")
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	for _, want := range []string{"GET /api/users", "SELECT users", "ERROR"} {
		if !strings.Contains(text.Text, want) {
			t.Errorf("waterfall missing %q:\n%s", want, text.Text)
		}
	}

	raw, _ := json.Marshal(res.StructuredContent)
	var out GetTraceOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.SpanCount != 2 {
		t.Errorf("span_count = %d, want 2", out.SpanCount)
	}
	if out.DurationMs != 301.0 {
		t.Errorf("duration_ms = %v, want 301", out.DurationMs)
	}
	if len(out.Issues) != 1 || out.Issues[0].Kind != "n_plus_one" {
		t.Errorf("issues = %+v, want one n_plus_one", out.Issues)
	}
	if out.Issues[0].WastedMs != 50.0 {
		t.Errorf("wasted_ms = %v, want 50", out.Issues[0].WastedMs)
	}
	if len(out.Logs) != 1 || out.Logs[0].Body != "query failed" {
		t.Errorf("logs = %+v, want one 'query failed'", out.Logs)
	}
	if out.Logs[0].Severity != "ERROR" {
		t.Errorf("log severity = %q, want ERROR", out.Logs[0].Severity)
	}
	// ListLintWarnings also surfaces detector findings (e.g. the n+1) as lint
	// entries, so just assert our seeded semconv warning is present.
	foundLint := false
	for _, l := range out.Lint {
		if l.RuleID == "db.system_required" {
			foundLint = true
		}
	}
	if !foundLint {
		t.Errorf("lint missing db.system_required: %+v", out.Lint)
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "get_trace", Arguments: map[string]any{"trace_id": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error for missing trace")
	}
}

func TestGetSpan(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	// db span: error status, db attributes, an exception event.
	db := callStructured[GetSpanOutput](t, cs, "get_span", map[string]any{"span_id": dbSpanID})
	if db.Status != "ERROR" {
		t.Errorf("status = %q, want ERROR", db.Status)
	}
	if db.Kind != "client" {
		t.Errorf("kind = %q, want client", db.Kind)
	}
	if db.Attributes["db.system"] != "postgresql" {
		t.Errorf("db.system = %q, want postgresql", db.Attributes["db.system"])
	}
	if len(db.Events) != 1 || db.Events[0].Name != "exception" {
		t.Errorf("events = %+v, want one exception", db.Events)
	}

	// root span: carries the outgoing link.
	root := callStructured[GetSpanOutput](t, cs, "get_span", map[string]any{"span_id": rootSpanID})
	if len(root.Links) != 1 || root.Links[0].LinkedTraceID != "other-trace" {
		t.Errorf("links = %+v, want one to other-trace", root.Links)
	}
}

func TestListSlowSpans(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListSlowSpansOutput](t, cs, "list_slow_spans", nil)
	if out.Count != 2 {
		t.Fatalf("expected 2 spans, got %d", out.Count)
	}
	// Slowest first: the 300ms db span.
	if out.Spans[0].SpanID != dbSpanID {
		t.Errorf("slowest span = %q, want %q", out.Spans[0].SpanID, dbSpanID)
	}
	if out.Spans[0].DurationMs != 300.0 {
		t.Errorf("slowest duration_ms = %v, want 300", out.Spans[0].DurationMs)
	}
	// Both spans belong to a trace with an n+1 issue, so they carry the n+1 tag.
	if out.Spans[0].Tag != "n+1" {
		t.Errorf("tag = %q, want n+1", out.Spans[0].Tag)
	}
}
