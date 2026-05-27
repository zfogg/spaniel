package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestListLogs_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	logs := decodeData[[]storage.Log](t, w.Body.Bytes())
	if len(logs) != 0 {
		t.Errorf("expected empty, got %d", len(logs))
	}
}

func TestListLogs_BySession(t *testing.T) {
	handler, store := setupRouter(t)
	insertLog(t, store, "trace-a", "span-a", "hello A", "svc", "sess-A")
	insertLog(t, store, "trace-b", "span-b", "hello B", "svc", "sess-B")
	insertLog(t, store, "trace-a", "span-a", "another A", "svc", "sess-A")

	w := do(t, handler, http.MethodGet, "/api/logs?sessionId=sess-A", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	logs := decodeData[[]storage.Log](t, w.Body.Bytes())
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs in sess-A, got %d", len(logs))
	}
	for _, l := range logs {
		if l.SessionID != "sess-A" {
			t.Errorf("leaked log from %s: %+v", l.SessionID, l)
		}
	}
}

func TestListLogs_ByTrace(t *testing.T) {
	handler, store := setupRouter(t)
	insertLog(t, store, "trace-a", "span-a", "A1", "svc", "sess")
	insertLog(t, store, "trace-a", "span-a", "A2", "svc", "sess")
	insertLog(t, store, "trace-b", "span-b", "B1", "svc", "sess")

	w := do(t, handler, http.MethodGet, "/api/logs?traceId=trace-a", nil)
	logs := decodeData[[]storage.Log](t, w.Body.Bytes())
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs for trace-a, got %d", len(logs))
	}
	for _, l := range logs {
		if l.TraceID != "trace-a" {
			t.Errorf("leaked trace %s", l.TraceID)
		}
	}
}

func TestListLogs_BySpan(t *testing.T) {
	handler, store := setupRouter(t)
	insertLog(t, store, "trace-a", "span-1", "S1", "svc", "sess")
	insertLog(t, store, "trace-a", "span-2", "S2", "svc", "sess")

	w := do(t, handler, http.MethodGet, "/api/logs?spanId=span-1", nil)
	logs := decodeData[[]storage.Log](t, w.Body.Bytes())
	if len(logs) != 1 || logs[0].SpanID != "span-1" {
		t.Errorf("expected one log for span-1, got %+v", logs)
	}
}

// Sanity: encoding/json is used, keeps the import lint-clean if the test
// file shrinks. (No-op assertion against the empty struct.)
var _ = json.RawMessage{}
