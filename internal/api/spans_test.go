package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func listSpansData(t *testing.T, handler http.Handler, query string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/spans"+query, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	var data []map[string]any
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("parse data: %v", err)
	}
	return data
}

func TestListSpans_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	data := listSpansData(t, handler, "")
	if len(data) != 0 {
		t.Errorf("expected 0 results, got %d", len(data))
	}
}

func TestListSpans_ReturnsAllFields(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-abc", "span-1", "", "svc-a", "GET /api/users")

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected at least 1 result")
	}
	row := data[0]
	for _, field := range []string{"span_id", "trace_id", "service_name", "name", "kind",
		"start_ns", "end_ns", "duration_ns", "status_code", "session_id"} {
		if _, ok := row[field]; !ok {
			t.Errorf("missing field %q in response", field)
		}
	}
}

func TestListSpans_SessionFilter(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-A", "trace-1", "span-a1", "", "svc", "op-alpha")
	insertSpan(t, store, "sess-B", "trace-2", "span-b1", "", "svc", "op-beta")

	data := listSpansData(t, handler, "?sessionId=sess-A")
	if len(data) != 1 {
		t.Fatalf("expected 1 result for sess-A, got %d", len(data))
	}
	if sid, _ := data[0]["session_id"].(string); sid != "sess-A" {
		t.Errorf("expected session_id=sess-A, got %q", sid)
	}
}

func TestListSpans_SortByDuration(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t1", SpanID: "s-fast", ServiceName: "svc", Name: "fast",
		StartNs: now, EndNs: now + 10_000_000, // 10ms
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t2", SpanID: "s-slow", ServiceName: "svc", Name: "slow",
		StartNs: now, EndNs: now + 500_000_000, // 500ms
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})

	data := listSpansData(t, handler, "?sort=dur")
	if len(data) < 2 {
		t.Fatalf("expected 2 results, got %d", len(data))
	}
	// First result should be the slowest span
	if name, _ := data[0]["name"].(string); name != "slow" {
		t.Errorf("expected slowest span first (sort=dur), got %q", name)
	}
}

func TestListSpans_SortByName(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-z", "span-z", "", "svc", "z-operation")
	insertSpan(t, store, "sess-1", "trace-a", "span-a", "", "svc", "a-operation")

	data := listSpansData(t, handler, "?sort=name")
	if len(data) < 2 {
		t.Fatalf("expected 2 results, got %d", len(data))
	}
	if name, _ := data[0]["name"].(string); name != "a-operation" {
		t.Errorf("expected alphabetical first span, got %q", name)
	}
}

func TestListSpans_TagError(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t-err", SpanID: "s-err", ServiceName: "svc", Name: "errored-op",
		StartNs: now, EndNs: now + 1_000_000,
		StatusCode: 2, // ERROR
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected a result")
	}
	tag, _ := data[0]["tag"].(string)
	if tag != "error" {
		t.Errorf("expected tag=error for status_code=2, got %q", tag)
	}
}

func TestListSpans_TagSlow(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t-slow", SpanID: "s-slow", ServiceName: "svc", Name: "slow-op",
		StartNs: now, EndNs: now + 300_000_000, // 300ms > 250ms threshold
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected a result")
	}
	tag, _ := data[0]["tag"].(string)
	if tag != "slow" {
		t.Errorf("expected tag=slow for 300ms span, got %q", tag)
	}
}

func TestListSpans_TagLint(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t-lint", SpanID: "s-lint", ServiceName: "svc", Name: "lint-op",
		StartNs: now, EndNs: now + 1_000_000, // fast, no error
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})
	store.InsertLintWarning(&storage.LintWarning{ //nolint:errcheck
		SpanID: "s-lint", TraceID: "t-lint", SessionID: "sess-1",
		RuleID: "SEMCONV.MISSING", Message: "missing attribute", Severity: "warn",
	})

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected a result")
	}
	tag, _ := data[0]["tag"].(string)
	if tag != "lint" {
		t.Errorf("expected tag=lint, got %q", tag)
	}
}

func TestListSpans_LimitParam(t *testing.T) {
	handler, store := setupRouter(t)
	for i := range 20 {
		insertSpan(t, store, "sess-1",
			fmt.Sprintf("trace-%d", i),
			fmt.Sprintf("span-%d", i),
			"", "svc", "op")
	}

	data := listSpansData(t, handler, "?limit=5")
	if len(data) > 5 {
		t.Errorf("expected ≤5 results with limit=5, got %d", len(data))
	}
}

func TestListSpans_NoTagForFastOkSpan(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t-ok", SpanID: "s-ok", ServiceName: "svc", Name: "fast-ok",
		StartNs: now, EndNs: now + 1_000_000, // 1ms, status OK
		StatusCode: 0,
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected a result")
	}
	// tag should be absent or empty string
	tag, _ := data[0]["tag"].(string)
	if tag != "" {
		t.Errorf("expected no tag for fast OK span, got %q", tag)
	}
}

func TestListSpans_TagPriorityN1OverSlow(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	// Insert span that is slow AND part of an n+1 trace
	store.InsertSpan(&storage.Span{ //nolint:errcheck
		TraceID: "t-n1", SpanID: "s-n1", ServiceName: "svc", Name: "slow-n1",
		StartNs: now, EndNs: now + 400_000_000, // 400ms = slow
		Attributes: "{}", Resource: "{}", SessionID: "sess-1", SessionLabel: "sess-1", ReceivedAt: now,
	})
	store.UpsertTraceIssue(&storage.TraceIssue{ //nolint:errcheck
		ID: "issue-1", TraceID: "t-n1", SessionID: "sess-1",
		Kind: "n_plus_one", Fingerprint: "fp1", Count: 5,
		WastedNs: 100_000_000, ParentSpanID: "s-n1", ExampleSpanID: "s-n1",
		CreatedAt: now,
	})

	data := listSpansData(t, handler, "")
	if len(data) == 0 {
		t.Fatal("expected a result")
	}
	tag, _ := data[0]["tag"].(string)
	if tag != "n+1" {
		t.Errorf("expected tag=n+1 to take priority over slow, got %q", tag)
	}
}

// TestGetSpan_EventsIsEmptyArrayWhenNone — a span with no events must still
// serialize the field as [], never null, so the frontend type can be a
// non-optional SpanEvent[].
func TestGetSpan_EventsIsEmptyArrayWhenNone(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-empty", "span-empty", "", "svc", "noop")

	req := httptest.NewRequest(http.MethodGet, "/api/spans/span-empty", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Errorf("expected `\"events\":[]` in response, got: %s", w.Body.String())
	}
}

// TestGetSpan_IncludesEvents verifies that GET /api/spans/:id surfaces the
// span's stored events so the inspector doesn't need a second round-trip.
func TestGetSpan_IncludesEvents(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-evt", "span-evt", "", "svc", "GET /things")

	// Two events, including one canonical exception event.
	now := int64(1_000_000_000)
	if err := store.InsertSpanEvents([]*storage.SpanEvent{
		{SpanID: "span-evt", TraceID: "trace-evt", SessionID: "sess-1",
			TimeNs: now + 400_000, Name: "pg.connection.acquired", Attributes: "{}"},
		{SpanID: "span-evt", TraceID: "trace-evt", SessionID: "sess-1",
			TimeNs: now + 7_800_000, Name: "exception",
			Attributes: `{"exception.type":"DBError","exception.message":"deadlock","exception.stacktrace":"at L1\nat L2"}`},
	}); err != nil {
		t.Fatalf("InsertSpanEvents: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/spans/span-evt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			SpanID string                  `json:"span_id"`
			Events []map[string]any        `json:"events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Data.SpanID != "span-evt" {
		t.Errorf("span_id: want span-evt, got %q", resp.Data.SpanID)
	}
	if len(resp.Data.Events) != 2 {
		t.Fatalf("events: want 2, got %d (%v)", len(resp.Data.Events), resp.Data.Events)
	}
	// Time order is enforced by ListEventsBySpan; first event should be the earlier one.
	if got, _ := resp.Data.Events[0]["name"].(string); got != "pg.connection.acquired" {
		t.Errorf("event[0].name: want pg.connection.acquired, got %q", got)
	}
	if got, _ := resp.Data.Events[1]["name"].(string); got != "exception" {
		t.Errorf("event[1].name: want exception, got %q", got)
	}
	// exception attributes should be preserved verbatim in the JSON string field.
	attrs := fmt.Sprint(resp.Data.Events[1]["attributes"])
	if !strings.Contains(attrs, "exception.stacktrace") {
		t.Errorf("expected exception.stacktrace in event attrs, got %q", attrs)
	}
}

// TestGetSpan_LinksIsEmptyArrayWhenNone — the links field is always [] on
// the wire, mirroring the events guarantee, so the frontend can type it as
// a non-optional SpanLink[].
func TestGetSpan_LinksIsEmptyArrayWhenNone(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-noli", "span-noli", "", "svc", "noop")

	req := httptest.NewRequest(http.MethodGet, "/api/spans/span-noli", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"links":[]`) {
		t.Errorf("expected `\"links\":[]` in response, got: %s", w.Body.String())
	}
}

// TestGetSpan_IncludesLinks: GET /api/spans/:id surfaces the span's links
// in insertion order with attributes + trace_state round-tripped.
func TestGetSpan_IncludesLinks(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-li", "span-li", "", "svc", "kafka.consume")

	if err := store.InsertSpanLinks([]*storage.SpanLink{
		{SpanID: "span-li", TraceID: "trace-li", SessionID: "sess-1",
			LinkedTraceID: "trace-prod-a", LinkedSpanID: "span-prod-a",
			TraceState: "rojo=00f067aa0ba902b7",
			Attributes:  `{"messaging.operation":"process"}`},
		{SpanID: "span-li", TraceID: "trace-li", SessionID: "sess-1",
			LinkedTraceID: "trace-prod-b", LinkedSpanID: "span-prod-b",
			Attributes:  "{}"},
	}); err != nil {
		t.Fatalf("InsertSpanLinks: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/spans/span-li", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Links []map[string]any `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resp.Data.Links) != 2 {
		t.Fatalf("links: want 2, got %d (%v)", len(resp.Data.Links), resp.Data.Links)
	}
	if got, _ := resp.Data.Links[0]["linked_trace_id"].(string); got != "trace-prod-a" {
		t.Errorf("links[0].linked_trace_id: want trace-prod-a, got %q", got)
	}
	if got, _ := resp.Data.Links[0]["trace_state"].(string); got != "rojo=00f067aa0ba902b7" {
		t.Errorf("links[0].trace_state lost: %q", got)
	}
	if attrs := fmt.Sprint(resp.Data.Links[0]["attributes"]); !strings.Contains(attrs, "messaging.operation") {
		t.Errorf("links[0].attributes lost: %q", attrs)
	}
}

// TestListIncomingLinks: GET /api/traces/:id/incoming-links returns spans
// whose link targets the given trace.
func TestListIncomingLinks(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-caller", "span-caller", "", "svc", "consume")
	if err := store.InsertSpanLinks([]*storage.SpanLink{
		{SpanID: "span-caller", TraceID: "trace-caller", SessionID: "sess-1",
			LinkedTraceID: "trace-target", LinkedSpanID: "span-target", Attributes: "{}"},
	}); err != nil {
		t.Fatalf("InsertSpanLinks: %v", err)
	}

	// match
	req := httptest.NewRequest(http.MethodGet, "/api/traces/trace-target/incoming-links", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 1 {
		t.Fatalf("want 1 incoming link, got %d", len(resp.Data))
	}
	if got, _ := resp.Data[0]["span_id"].(string); got != "span-caller" {
		t.Errorf("incoming link's span_id: want span-caller, got %q", got)
	}

	// non-matching trace id → empty array
	req = httptest.NewRequest(http.MethodGet, "/api/traces/trace-nobody/incoming-links", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("expected empty data array, got: %s", w.Body.String())
	}
}
