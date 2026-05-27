package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// insertSpanFull lets these tests set durations explicitly (the search_test.go
// insertSpan helper hard-codes a 1ms duration which is fine for everything else
// but defeats the avg_duration_ns assertion in the parent-child edge test).
func insertSpanFull(t *testing.T, store *storage.DB, sessID, traceID, spanID, parent, svc, name string, durNs int64) {
	t.Helper()
	now := time.Now().UnixNano()
	if err := store.InsertSpan(&storage.Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		StartNs:      now,
		EndNs:        now + durNs,
		Attributes:   "{}",
		Resource:     "{}",
		SessionID:    sessID,
		SessionLabel: sessID,
		ReceivedAt:   now,
	}); err != nil {
		t.Fatalf("InsertSpan: %v", err)
	}
}

func TestGetServiceMap_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/service-map", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	data := decodeData[storage.ServiceMapData](t, w.Body.Bytes())
	if len(data.Nodes) != 0 || len(data.Edges) != 0 {
		t.Errorf("expected empty map, got nodes=%d edges=%d", len(data.Nodes), len(data.Edges))
	}
}

func TestGetServiceMap_SingleService(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess", "trace-a", "span-1", "", "api", "GET /x")
	insertSpan(t, store, "sess", "trace-a", "span-2", "span-1", "api", "GET /y")

	w := do(t, handler, http.MethodGet, "/api/service-map", nil)
	data := decodeData[storage.ServiceMapData](t, w.Body.Bytes())
	if len(data.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %+v", data.Nodes)
	}
	if data.Nodes[0].ID != "api" || data.Nodes[0].SpanCount != 2 {
		t.Errorf("node wrong: %+v", data.Nodes[0])
	}
	// All spans are same-service so the parent-child edge is suppressed.
	if len(data.Edges) != 0 {
		t.Errorf("expected 0 same-service edges, got %+v", data.Edges)
	}
}

func TestGetServiceMap_ParentChild(t *testing.T) {
	handler, store := setupRouter(t)
	// api → db, two calls with avg 30ms.
	insertSpanFull(t, store, "sess", "t1", "api-1",  "",       "api", "GET /cart", 100_000_000)
	insertSpanFull(t, store, "sess", "t1", "db-1",   "api-1",  "db",  "SELECT",     20_000_000)
	insertSpanFull(t, store, "sess", "t1", "db-2",   "api-1",  "db",  "SELECT",     40_000_000)

	w := do(t, handler, http.MethodGet, "/api/service-map", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	data := decodeData[storage.ServiceMapData](t, w.Body.Bytes())

	if len(data.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (api, db), got %+v", data.Nodes)
	}
	if len(data.Edges) != 1 {
		t.Fatalf("expected 1 edge api→db, got %+v", data.Edges)
	}
	e := data.Edges[0]
	if e.From != "api" || e.To != "db" {
		t.Errorf("edge direction wrong: %+v", e)
	}
	if e.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", e.CallCount)
	}
	if e.AvgDurationNs != 30_000_000 {
		t.Errorf("AvgDurationNs = %d, want 30_000_000", e.AvgDurationNs)
	}
}

func TestGetServiceMap_SessionFilter(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "keep",  "t1", "k1", "", "api", "op")
	insertSpan(t, store, "drop",  "t2", "d1", "", "other", "op")

	w := do(t, handler, http.MethodGet, "/api/service-map?sessionId=keep", nil)
	data := decodeData[storage.ServiceMapData](t, w.Body.Bytes())
	if len(data.Nodes) != 1 || data.Nodes[0].ID != "api" {
		t.Errorf("session filter leaked: %+v", data.Nodes)
	}
}
