package api

import (
	"net/http"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type fakeThroughput struct{ tp storage.Throughput }

func (f *fakeThroughput) Throughput() storage.Throughput { return f.tp }

type fakeDropCounters struct {
	spans   int64
	logs    int64
	metrics int64
	at      int64
}

func (f *fakeDropCounters) DroppedSpansTotal() int64        { return f.spans }
func (f *fakeDropCounters) DroppedLogsTotal() int64         { return f.logs }
func (f *fakeDropCounters) DroppedMetricPointsTotal() int64 { return f.metrics }
func (f *fakeDropCounters) LastDropAt() int64               { return f.at }

func setupRouterWithTP(t *testing.T, tp ThroughputProvider) (http.Handler, *storage.DB) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	hub := ws.NewHub()
	handler := NewRouterFull(store, hub, nil, nil, nil, tp, nil, nil)
	return handler, store
}

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

func TestGetStats_IncludesThroughput(t *testing.T) {
	tp := &fakeThroughput{tp: storage.Throughput{
		SpansPerSec:     12.5,
		LogsPerSec:      3.0,
		MetricsPerSec:   1.0,
		PeakSpansPerSec: 40.0,
	}}
	handler, store := setupRouterWithTP(t, tp)
	insertSpan(t, store, "sess", "trace-1", "span-1", "", "svc", "op")

	w := do(t, handler, http.MethodGet, "/api/stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	s := decodeData[storage.Stats](t, w.Body.Bytes())
	if s.SpansPerSec != 12.5 {
		t.Errorf("SpansPerSec = %v, want 12.5", s.SpansPerSec)
	}
	if s.PeakSpansPerSec != 40.0 {
		t.Errorf("PeakSpansPerSec = %v, want 40.0", s.PeakSpansPerSec)
	}
}

func TestGetStats_IncludesDropCounters(t *testing.T) {
	dc := &fakeDropCounters{spans: 12, logs: 3, metrics: 7, at: 1_000_000}
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	hub := ws.NewHub()
	handler := NewRouterFull(store, hub, nil, nil, nil, nil, dc, nil)

	w := do(t, handler, http.MethodGet, "/api/stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	s := decodeData[storage.Stats](t, w.Body.Bytes())
	if s.DroppedSpans != 12 {
		t.Errorf("DroppedSpans = %d, want 12", s.DroppedSpans)
	}
	if s.DroppedLogs != 3 {
		t.Errorf("DroppedLogs = %d, want 3", s.DroppedLogs)
	}
	if s.DroppedMetricPoints != 7 {
		t.Errorf("DroppedMetricPoints = %d, want 7", s.DroppedMetricPoints)
	}
	if s.LastDropAt != 1_000_000 {
		t.Errorf("LastDropAt = %d, want 1000000", s.LastDropAt)
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
