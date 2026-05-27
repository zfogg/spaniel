package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"

	_ "github.com/marcboeker/go-duckdb"
)

func setupRouter(t *testing.T) (http.Handler, *storage.DB) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	hub := ws.NewHub()
	handler := NewRouter(store, hub, nil)
	return handler, store
}

func TestHealthEndpoint(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ok") {
		t.Errorf("expected body to contain 'ok', got: %s", body)
	}
}

func TestListTracesEmpty(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	dataRaw, ok := resp["data"]
	if !ok {
		t.Fatalf("response missing 'data' field")
	}

	var data []any
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		t.Fatalf("expected data to be an array: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestGetIssuesRequiresTraceId(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 when traceId is missing, got %d", w.Code)
	}
}

func TestGetIssuesEmpty(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/issues?traceId=nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	dataRaw, ok := resp["data"]
	if !ok {
		t.Fatalf("response missing 'data' field")
	}

	var data []any
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		t.Fatalf("expected data to be an array: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array for nonexistent traceId, got %d items", len(data))
	}
}

func TestGetIssuesReturnsResults(t *testing.T) {
	handler, store := setupRouter(t)

	traceID := "trace-with-issue"
	issue := &storage.TraceIssue{
		ID:            "test-issue-1",
		TraceID:       traceID,
		SessionID:     "sess-1",
		Kind:          "n_plus_one",
		Fingerprint:   "SELECT * FROM users WHERE id = ?",
		Count:         12,
		WastedNs:      300000,
		ParentSpanID:  "parent-span-id",
		ExampleSpanID: "example-span-id",
		CreatedAt:     time.Now().UnixNano(),
	}
	if err := store.UpsertTraceIssue(issue); err != nil {
		t.Fatalf("UpsertTraceIssue failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/issues?traceId="+traceID, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	dataRaw, ok := resp["data"]
	if !ok {
		t.Fatalf("response missing 'data' field")
	}

	var data []map[string]any
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		t.Fatalf("expected data to be an array of objects: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(data))
	}
	if id, _ := data[0]["id"].(string); id != "test-issue-1" {
		t.Errorf("expected issue id=test-issue-1, got %q", id)
	}
	if kind, _ := data[0]["kind"].(string); kind != "n_plus_one" {
		t.Errorf("expected issue kind=n_plus_one, got %q", kind)
	}
}

func TestListForwardersEmpty(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/forwarders", nil)
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
		t.Errorf("expected empty forwarders list, got %d", len(data))
	}
}

func TestListForwardersWithUpstreams(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	hub := ws.NewHub()

	// Stand up a fake upstream that always succeeds.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	fwd := forwarder.New([]string{upstream.URL})
	// Send one payload so the sent counter is non-zero.
	fwd.Forward("/v1/traces", "application/json", []byte("{}"))

	// Wait for the async send to complete.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && fwd.Status()[0].Sent == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	handler := NewRouter(store, hub, fwd)
	req := httptest.NewRequest(http.MethodGet, "/api/forwarders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	var data []map[string]any
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("parse data: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 forwarder, got %d", len(data))
	}
	if sent, _ := data[0]["sent"].(float64); sent != 1 {
		t.Errorf("expected sent=1, got %v", data[0]["sent"])
	}
	if data[0]["url"] != upstream.URL {
		t.Errorf("expected url=%q, got %q", upstream.URL, data[0]["url"])
	}
}

func TestListLintEmpty(t *testing.T) {
	handler, _ := setupRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/lint", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	dataRaw, ok := resp["data"]
	if !ok {
		t.Fatalf("response missing 'data' field")
	}

	var data []any
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		t.Fatalf("expected data to be an array: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}
