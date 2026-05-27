package api

import (
	"net/http"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestGetStats_ZeroWhenEmpty(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	s := decodeData[storage.Stats](t, w.Body.Bytes())
	if s.SpanCount != 0 || s.TraceCount != 0 || s.LogCount != 0 {
		t.Errorf("expected zero counts, got %+v", s)
	}
}

func TestGetStats_AfterIngestion(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess", "trace-1", "span-1", "", "svc", "GET /a")
	insertSpan(t, store, "sess", "trace-1", "span-2", "span-1", "svc", "GET /b")
	insertSpan(t, store, "sess", "trace-2", "span-3", "", "svc", "GET /c")
	insertLog(t, store, "trace-1", "span-1", "info", "svc", "sess")
	insertLog(t, store, "trace-2", "span-3", "warn", "svc", "sess")

	w := do(t, handler, http.MethodGet, "/api/stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	s := decodeData[storage.Stats](t, w.Body.Bytes())
	if s.SpanCount != 3 {
		t.Errorf("SpanCount = %d, want 3", s.SpanCount)
	}
	if s.TraceCount != 2 {
		t.Errorf("TraceCount = %d, want 2", s.TraceCount)
	}
	if s.LogCount != 2 {
		t.Errorf("LogCount = %d, want 2", s.LogCount)
	}
}

func TestGetStats_SessionScopedCounts(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-A", "trace-a", "s-a", "", "svc", "op")
	insertSpan(t, store, "sess-B", "trace-b", "s-b", "", "svc", "op")
	insertSpan(t, store, "sess-B", "trace-b2", "s-b2", "", "svc", "op")

	w := do(t, handler, http.MethodGet, "/api/stats?sessionId=sess-B", nil)
	s := decodeData[storage.Stats](t, w.Body.Bytes())
	if s.SpanCount != 2 {
		t.Errorf("session-scoped SpanCount = %d, want 2", s.SpanCount)
	}
}
