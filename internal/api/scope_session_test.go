package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// getData issues a GET and returns the decoded data array.
func getData(t *testing.T, handler http.Handler, path string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, w.Code, w.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var data []map[string]any
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("parse %s data: %v", path, err)
	}
	return data
}

// With an active session set, endpoints with no explicit sessionId return only
// the active session's data; an explicit sessionId still overrides.
func TestEndpointsScopeToActiveSession(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-A", "trace-a", "span-a", "", "svc-a", "op-a")
	insertSpan(t, store, "sess-B", "trace-b", "span-b", "", "svc-b", "op-b")
	store.SetActiveSession("sess-A", "A")

	// /api/traces → only the active session's trace.
	traces := getData(t, handler, "/api/traces")
	if len(traces) != 1 || traces[0]["trace_id"] != "trace-a" {
		t.Errorf("/api/traces scoped: got %v, want only trace-a", traces)
	}
	// explicit sessionId overrides the active default.
	other := getData(t, handler, "/api/traces?sessionId=sess-B")
	if len(other) != 1 || other[0]["trace_id"] != "trace-b" {
		t.Errorf("/api/traces?sessionId=sess-B: got %v, want trace-b", other)
	}

	// /api/spans → only active session.
	spans := getData(t, handler, "/api/spans")
	if len(spans) != 1 || spans[0]["span_id"] != "span-a" {
		t.Errorf("/api/spans scoped: got %v, want only span-a", spans)
	}

	// /api/services → only active session's service (string array).
	if got := getStringData(t, handler, "/api/services"); len(got) != 1 || got[0] != "svc-a" {
		t.Errorf("/api/services scoped: got %v, want [svc-a]", got)
	}
}

func getStringData(t *testing.T, handler http.Handler, path string) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var data []string
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("parse %s data: %v", path, err)
	}
	return data
}

// With no active session set (active id == ""), scopeSession leaves it empty and
// endpoints return all sessions — preserving prior behavior.
func TestNoActiveSessionReturnsAll(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-A", "trace-a", "span-a", "", "svc-a", "op-a")
	insertSpan(t, store, "sess-B", "trace-b", "span-b", "", "svc-b", "op-b")
	// no SetActiveSession call

	traces := getData(t, handler, "/api/traces")
	if len(traces) != 2 {
		t.Errorf("no active session: expected all 2 traces, got %d", len(traces))
	}
}
