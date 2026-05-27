package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
