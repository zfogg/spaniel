package api

import (
	"net/http"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type fakeSources struct{ rows []storage.SourceStats }

func (f *fakeSources) Sources() []storage.SourceStats { return f.rows }

func TestListSources_Empty(t *testing.T) {
	handler, _ := setupRouter(t)
	w := do(t, handler, http.MethodGet, "/api/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())
	if len(rows) != 0 {
		t.Fatalf("expected empty list when no sources provider wired, got %d rows", len(rows))
	}
}

func TestListSources_WithFakeProvider(t *testing.T) {
	sp := &fakeSources{rows: []storage.SourceStats{
		{Service: "checkout", AcceptedPerSec: 12.5, RejectedPerSec: 0, ErrorRate: 0.02},
		{Service: "payments", AcceptedPerSec: 5.0, RejectedPerSec: 3.1, ErrorRate: 0.10},
	}}
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	handler := NewRouterFull(store, ws.NewHub(), nil, nil, nil, nil, nil, sp)

	w := do(t, handler, http.MethodGet, "/api/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(rows))
	}
	byService := map[string]storage.SourceStats{}
	for _, r := range rows {
		byService[r.Service] = r
	}
	if byService["checkout"].AcceptedPerSec != 12.5 {
		t.Errorf("checkout accepted_per_sec = %v, want 12.5", byService["checkout"].AcceptedPerSec)
	}
	if byService["payments"].RejectedPerSec != 3.1 {
		t.Errorf("payments rejected_per_sec = %v, want 3.1", byService["payments"].RejectedPerSec)
	}
}

func TestListSources_LivePipeline(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	sess, _ := store.CreateSession("src-test", false)
	store.SetActiveSession(sess.ID, sess.Label)

	limiter := ingestion.NewSourceLimiter(0, 0)
	sampler := ingestion.NewSampler(0, 0)
	p := ingestion.NewPipelineFull(store, ws.NewHub(), sampler, limiter)

	// Pump spans from two services through the limiter directly.
	for range 30 {
		limiter.Allow("svc-x", false, 200)
	}
	for range 15 {
		limiter.Allow("svc-y", true, 100)
	}

	handler := NewRouterFull(store, ws.NewHub(), nil, nil, nil, nil, nil, p)
	w := do(t, handler, http.MethodGet, "/api/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	rows := decodeData[[]storage.SourceStats](t, w.Body.Bytes())

	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Service] = true
		if r.AcceptedPerSec <= 0 {
			t.Errorf("service %q has accepted_per_sec=0", r.Service)
		}
	}
	if !seen["svc-x"] || !seen["svc-y"] {
		t.Errorf("expected both svc-x and svc-y in sources, got %v", seen)
	}
}
