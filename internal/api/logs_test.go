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

func TestListLogs_BySeverity(t *testing.T) {
	handler, store := setupRouter(t)
	now := int64(1_000_000_000)
	for _, tc := range []struct {
		severity int
		body     string
	}{
		{9, "info"},
		{13, "warn-low"},
		{16, "warn-high"},
		{17, "error-low"},
		{20, "error-high"},
		{21, "fatal"},
	} {
		if err := store.InsertLog(&storage.Log{
			TimestampNs: now + int64(tc.severity),
			TraceID:     "trace", SpanID: tc.body, Severity: tc.severity,
			Body: tc.body, Attributes: "{}", ServiceName: "svc",
			SessionID: "sess", ReceivedAt: now,
		}); err != nil {
			t.Fatalf("InsertLog: %v", err)
		}
	}

	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"warn", []string{"warn-high", "warn-low"}},
		{"error", []string{"error-high", "error-low"}},
		{"fatal", []string{"fatal"}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			w := do(t, handler, http.MethodGet, "/api/logs?sessionId=sess&severity="+tc.filter, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
			}
			logs := decodeData[[]storage.Log](t, w.Body.Bytes())
			if len(logs) != len(tc.want) {
				t.Fatalf("expected %d logs, got %+v", len(tc.want), logs)
			}
			for i, want := range tc.want {
				if logs[i].Body != want {
					t.Errorf("log %d: want %q, got %q", i, want, logs[i].Body)
				}
			}
		})
	}
}

func TestListLogs_InvalidSeverity(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/logs?severity=nope", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// Sanity: encoding/json is used, keeps the import lint-clean if the test
// file shrinks. (No-op assertion against the empty struct.)
var _ = json.RawMessage{}
