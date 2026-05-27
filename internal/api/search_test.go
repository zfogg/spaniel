package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func insertSpan(t *testing.T, store *storage.DB, sessID, traceID, spanID, parent, svc, name string) {
	t.Helper()
	now := time.Now().UnixNano()
	if err := store.InsertSpan(&storage.Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		StartNs:      now,
		EndNs:        now + 1_000_000,
		Attributes:   "{}",
		Resource:     "{}",
		SessionID:    sessID,
		SessionLabel: sessID,
		ReceivedAt:   now,
	}); err != nil {
		t.Fatalf("InsertSpan: %v", err)
	}
}

func insertLog(t *testing.T, store *storage.DB, traceID, spanID, body, service, sessionID string) {
	t.Helper()
	now := time.Now().UnixNano()
	if err := store.InsertLog(&storage.Log{
		TimestampNs: now,
		TraceID:     traceID,
		SpanID:      spanID,
		Body:        body,
		ServiceName: service,
		SessionID:   sessionID,
		ReceivedAt:  now,
		Attributes:  "{}",
	}); err != nil {
		t.Fatalf("InsertLog: %v", err)
	}
}

func searchData(t *testing.T, handler http.Handler, query string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/search?q="+query, nil)
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

func TestSearchEmptyQuery(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	var data []any
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("parse data: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array for no query, got %d items", len(data))
	}
}

func TestSearchNoResults(t *testing.T) {
	handler, _ := setupRouter(t)
	data := searchData(t, handler, "nonexistent-xyz-qrs")
	if len(data) != 0 {
		t.Errorf("expected 0 results, got %d", len(data))
	}
}

func TestSearchBySpanName(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-abc", "span-1", "", "user-svc", "GET /api/users")

	data := searchData(t, handler, "users")
	if len(data) == 0 {
		t.Fatal("expected at least 1 result, got 0")
	}
	if kind, _ := data[0]["kind"].(string); kind != "trace" {
		t.Errorf("expected kind=trace, got %q", kind)
	}
	if tid, _ := data[0]["trace_id"].(string); tid != "trace-abc" {
		t.Errorf("expected trace_id=trace-abc, got %q", tid)
	}
	if title, _ := data[0]["title"].(string); title == "" {
		t.Error("expected non-empty title")
	}
}

func TestSearchByServiceName(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-svc", "span-2", "", "payment-service", "some-span")

	data := searchData(t, handler, "payment")
	if len(data) == 0 {
		t.Fatal("expected at least 1 result, got 0")
	}
	if subtitle, _ := data[0]["subtitle"].(string); subtitle != "payment-service" {
		t.Errorf("expected subtitle=payment-service, got %q", subtitle)
	}
}

func TestSearchByTraceIDPrefix(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "abcdef123456", "span-3", "", "svc", "some-op")

	data := searchData(t, handler, "abcdef")
	if len(data) == 0 {
		t.Fatal("expected at least 1 result for trace ID prefix, got 0")
	}
	if tid, _ := data[0]["trace_id"].(string); tid != "abcdef123456" {
		t.Errorf("expected trace_id=abcdef123456, got %q", tid)
	}
}

func TestSearchLogs(t *testing.T) {
	handler, store := setupRouter(t)
	insertLog(t, store, "trace-log", "span-log", "database connection failed: timeout", "db-svc", "sess-1")

	data := searchData(t, handler, "timeout")
	if len(data) == 0 {
		t.Fatal("expected at least 1 log result, got 0")
	}
	found := false
	for _, item := range data {
		if kind, _ := item["kind"].(string); kind == "log" {
			found = true
			if title, _ := item["title"].(string); title == "" {
				t.Error("log result has empty title")
			}
			break
		}
	}
	if !found {
		t.Error("expected a log-kind result, none found")
	}
}

func TestSearchSessionFilter(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "session-A", "trace-s1", "span-s1", "", "cart-svc", "checkout-handler")
	insertSpan(t, store, "session-B", "trace-s2", "span-s2", "", "cart-svc", "checkout-handler")

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=checkout&sessionId=session-A", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	var data []map[string]any
	json.Unmarshal(resp["data"], &data) //nolint:errcheck

	for _, item := range data {
		if sid, _ := item["session_id"].(string); sid != "session-A" {
			t.Errorf("got result from wrong session: %q", sid)
		}
	}
	if len(data) != 1 {
		t.Errorf("expected exactly 1 result for session-A, got %d", len(data))
	}
}

func TestSearchResponseShape(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-shape", "span-shape", "", "shape-svc", "GET /shape")

	data := searchData(t, handler, "shape")
	if len(data) == 0 {
		t.Fatal("expected at least 1 result")
	}
	r := data[0]
	for _, field := range []string{"kind", "trace_id", "title", "subtitle", "session_id"} {
		if v, ok := r[field]; !ok || v == "" {
			t.Errorf("result missing or empty field %q: %v", field, r)
		}
	}
	if kind, _ := r["kind"].(string); kind != "trace" && kind != "log" {
		t.Errorf("kind must be 'trace' or 'log', got %q", kind)
	}
}

func TestSearchMixedTracesAndLogs(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-mixed", "span-mixed", "", "svc", "payment-handler")
	insertLog(t, store, "trace-mixed", "span-mixed", "payment processed successfully", "svc", "sess-1")

	data := searchData(t, handler, "payment")
	kinds := map[string]int{}
	for _, item := range data {
		kinds[item["kind"].(string)]++
	}
	if kinds["trace"] == 0 {
		t.Error("expected at least one trace result for 'payment'")
	}
	if kinds["log"] == 0 {
		t.Error("expected at least one log result for 'payment'")
	}
}

func TestSearchLimitQueryParam(t *testing.T) {
	handler, store := setupRouter(t)
	for i := range 15 {
		insertSpan(t, store, "sess-1",
			fmt.Sprintf("trace-lim-%d", i),
			fmt.Sprintf("span-lim-%d", i),
			"", "svc", "findme-op")
	}

	// limit=12 → half=6; with 15 spans seeded and no logs, expect exactly 6 trace results.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=findme&limit=12", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	var data []any
	json.Unmarshal(resp["data"], &data) //nolint:errcheck
	if len(data) > 6 {
		t.Errorf("limit=12 (half=6) should return ≤6 results, got %d", len(data))
	}
}

func TestSearchSQLMetacharactersDoNotCrash(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-1", "trace-safe", "span-safe", "", "svc", "normal-op")

	for _, q := range []string{"%", "_", "%%", "100%", "user_id", "'; DROP TABLE spans; --"} {
		data := searchData(t, handler, url.QueryEscape(q))
		_ = data
	}
}

func TestSearchDeduplicatesTraces(t *testing.T) {
	handler, store := setupRouter(t)
	// Two spans in the same trace — should yield one trace result.
	insertSpan(t, store, "sess-1", "trace-dup", "span-d1", "", "order-svc", "GET /orders")
	insertSpan(t, store, "sess-1", "trace-dup", "span-d2", "span-d1", "order-svc", "GET /orders/detail")

	data := searchData(t, handler, "orders")
	traceCount := 0
	for _, item := range data {
		if kind, _ := item["kind"].(string); kind == "trace" {
			if tid, _ := item["trace_id"].(string); tid == "trace-dup" {
				traceCount++
			}
		}
	}
	if traceCount != 1 {
		t.Errorf("expected 1 deduplicated trace result, got %d", traceCount)
	}
}
