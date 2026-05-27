package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// diffSeedSpan inserts a span with a specific duration so we can assert on
// the diff's per-operation delta_pct without relying on the fixed-duration
// helper from search_test.go.
func diffSeedSpan(t *testing.T, store *storage.DB, sessID, traceID, spanID, parent, svc, name string, durNs int64) {
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
		DurationNs:   durNs,
		Attributes:   "{}",
		Resource:     "{}",
		SessionID:    sessID,
		SessionLabel: sessID,
		ReceivedAt:   now,
	}); err != nil {
		t.Fatalf("InsertSpan: %v", err)
	}
}

func TestGetDiff_MissingParams(t *testing.T) {
	handler, _ := setupRouter(t)
	for _, q := range []string{"", "?baseline=a", "?compare=b"} {
		w := do(t, handler, http.MethodGet, "/api/diff"+q, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: expected 400, got %d", q, w.Code)
		}
	}
}

func TestGetDiff_BaselineSessionNotFound(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("cmp", false)
	w := do(t, handler, http.MethodGet, "/api/diff?baseline=ghost&compare="+s.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing baseline session, got %d", w.Code)
	}
}

func TestGetDiff_SameSession(t *testing.T) {
	handler, store := setupRouter(t)
	s, _ := store.CreateSession("only", false)
	diffSeedSpan(t, store, s.ID, "t1", "root", "", "api", "GET /cart", 100_000_000)

	w := do(t, handler, http.MethodGet, "/api/diff?baseline="+s.ID+"&compare="+s.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	d := decodeData[DiffResult](t, w.Body.Bytes())
	if d.Summary.DurationDeltaNs != 0 || d.Summary.SpansAdded != 0 || d.Summary.SpansRemoved != 0 {
		t.Errorf("same-session diff should be all zeros, got %+v", d.Summary)
	}
	if d.Baseline.SpanCount != 1 || d.Compare.SpanCount != 1 {
		t.Errorf("span counts wrong: %+v / %+v", d.Baseline, d.Compare)
	}
}

func TestGetDiff_AcrossSessions(t *testing.T) {
	handler, store := setupRouter(t)
	base, _ := store.CreateSession("baseline", true)
	cmp, _ := store.CreateSession("compare", false)

	// Baseline: 100ms root, 30ms child.
	diffSeedSpan(t, store, base.ID, "tb", "br", "",   "api", "GET /cart", 100_000_000)
	diffSeedSpan(t, store, base.ID, "tb", "bc", "br", "db",  "SELECT",     30_000_000)

	// Compare: 150ms root, 90ms child (regression) + a new added span.
	diffSeedSpan(t, store, cmp.ID, "tc", "cr", "",   "api", "GET /cart",      150_000_000)
	diffSeedSpan(t, store, cmp.ID, "tc", "cc", "cr", "db",  "SELECT",          90_000_000)
	diffSeedSpan(t, store, cmp.ID, "tc", "cn", "cr", "api", "POST /checkout",   20_000_000)

	w := do(t, handler, http.MethodGet, "/api/diff?baseline="+base.ID+"&compare="+cmp.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", w.Code, w.Body.String())
	}
	d := decodeData[DiffResult](t, w.Body.Bytes())

	if d.Summary.SpansAdded != 1 {
		t.Errorf("expected 1 added span, got %d", d.Summary.SpansAdded)
	}
	if d.Summary.DurationDeltaNs != 50_000_000 {
		t.Errorf("root delta = %d, want 50ms", d.Summary.DurationDeltaNs)
	}
	// 50ms / 100ms → 50% (rounded to one decimal).
	if d.Summary.DurationDeltaPct != 50.0 {
		t.Errorf("DurationDeltaPct = %v, want 50", d.Summary.DurationDeltaPct)
	}
	// The added POST /checkout must appear with status=added.
	var addedFound bool
	for _, s := range d.Spans {
		if s.Name == "POST /checkout" && s.Status == "added" {
			addedFound = true
		}
	}
	if !addedFound {
		t.Errorf("expected POST /checkout to be tagged 'added'; got spans=%+v", d.Spans)
	}
}
