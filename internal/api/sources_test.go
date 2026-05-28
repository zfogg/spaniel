package api

import (
	"net/http"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestListSources_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())
	if len(rows) != 0 {
		t.Fatalf("expected empty list with no spans, got %d rows", len(rows))
	}
}

func TestListSources_FromDB(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess", "trace-1", "span-1", "", "checkout", "GET /cart")
	insertSpan(t, store, "sess", "trace-1", "span-2", "span-1", "checkout", "GET /cart")
	insertSpan(t, store, "sess", "trace-2", "span-3", "", "payments", "POST /charge")

	w := do(t, handler, http.MethodGet, "/api/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 services, got %d: %+v", len(rows), rows)
	}

	byService := map[string]storage.SourceStats{}
	for _, r := range rows {
		byService[r.Service] = r
	}
	if byService["checkout"].AcceptedPerSec <= 0 {
		t.Errorf("checkout accepted_per_sec should be > 0, got %v", byService["checkout"].AcceptedPerSec)
	}
	if byService["payments"].AcceptedPerSec <= 0 {
		t.Errorf("payments accepted_per_sec should be > 0, got %v", byService["payments"].AcceptedPerSec)
	}
	// checkout has more spans so should sort first
	if rows[0].Service != "checkout" {
		t.Errorf("expected checkout first (most spans), got %q", rows[0].Service)
	}
}

func TestListSources_SessionFilter(t *testing.T) {
	handler, store := setupRouter(t)
	insertSpan(t, store, "sess-A", "trace-a", "span-a", "", "svc-alpha", "op")
	insertSpan(t, store, "sess-B", "trace-b", "span-b", "", "svc-beta", "op")

	w := do(t, handler, http.MethodGet, "/api/sources?sessionId=sess-A", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())
	if len(rows) != 1 {
		t.Fatalf("expected 1 service for sess-A, got %d", len(rows))
	}
	if rows[0].Service != "svc-alpha" {
		t.Errorf("expected svc-alpha, got %q", rows[0].Service)
	}
}
