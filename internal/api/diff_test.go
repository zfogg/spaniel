package api

import (
	"math"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// span is a tiny builder for *storage.Span used in diff tests.
func span(svc, name, spanID, parent string, durNs int64, attrs string) *storage.Span {
	return &storage.Span{
		TraceID:      "t1",
		SpanID:       spanID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		StartNs:      0,
		EndNs:        durNs,
		DurationNs:   durNs,
		Attributes:   attrs,
	}
}

// rootSpan helper — root has parent set to zeroParent so isRoot() reports true.
func rootSpan(svc, name, spanID string, durNs int64) *storage.Span {
	return span(svc, name, spanID, zeroParent, durNs, "{}")
}

func sess(id, label string) *storage.Session {
	return &storage.Session{ID: id, Label: label}
}

// findDiffSpan returns the DiffSpan matching (svc,name), or fails the test.
func findDiffSpan(t *testing.T, result DiffResult, svc, name string) DiffSpan {
	t.Helper()
	for _, s := range result.Spans {
		if s.ServiceName == svc && s.Name == name {
			return s
		}
	}
	t.Fatalf("no diff span for %s/%s in %+v", svc, name, result.Spans)
	return DiffSpan{}
}

// Baseline 3 spans, compare 4 (1 new) — expect exactly 1 added span and the
// new span reported with status="added".
func TestComputeDiff_AddedSpan(t *testing.T) {
	baseSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch user", "s2", "s1", 30_000_000, "{}"),
		span("api", "fetch cart", "s3", "s1", 40_000_000, "{}"),
	}
	cmpSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch user", "s2", "s1", 30_000_000, "{}"),
		span("api", "fetch cart", "s3", "s1", 40_000_000, "{}"),
		span("api", "fetch promo", "s4", "s1", 20_000_000, "{}"),
	}

	result := computeDiff(sess("a", "base"), sess("b", "cmp"), baseSpans, cmpSpans)

	if result.Summary.SpansAdded != 1 {
		t.Errorf("SpansAdded: want 1, got %d", result.Summary.SpansAdded)
	}
	if result.Summary.SpansRemoved != 0 {
		t.Errorf("SpansRemoved: want 0, got %d", result.Summary.SpansRemoved)
	}
	added := findDiffSpan(t, result, "api", "fetch promo")
	if added.Status != "added" {
		t.Errorf("status: want added, got %q", added.Status)
	}
	if added.BaselineDurationNs != 0 || added.CompareDurationNs != 20_000_000 {
		t.Errorf("added span durations wrong: %+v", added)
	}
}

// 50% duration increase on a matching span — expect status="changed" and
// DeltaPct = +50.0.
func TestComputeDiff_DurationIncreasePct(t *testing.T) {
	baseSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch user", "s2", "s1", 20_000_000, "{}"),
	}
	cmpSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 150_000_000),
		span("api", "fetch user", "s2", "s1", 30_000_000, "{}"),
	}

	result := computeDiff(sess("a", "base"), sess("b", "cmp"), baseSpans, cmpSpans)

	user := findDiffSpan(t, result, "api", "fetch user")
	if user.Status != "changed" {
		t.Errorf("status: want changed, got %q", user.Status)
	}
	if math.Abs(user.DeltaPct-50.0) > 0.01 {
		t.Errorf("DeltaPct: want 50.0, got %v", user.DeltaPct)
	}
	if user.BaselineDurationNs != 20_000_000 || user.CompareDurationNs != 30_000_000 {
		t.Errorf("durations wrong: %+v", user)
	}

	// Summary uses the root span duration: base=100ms cmp=150ms => +50%.
	if result.Summary.DurationDeltaNs != 50_000_000 {
		t.Errorf("DurationDeltaNs: want 50ms, got %d", result.Summary.DurationDeltaNs)
	}
	if math.Abs(result.Summary.DurationDeltaPct-50.0) > 0.01 {
		t.Errorf("DurationDeltaPct: want 50.0, got %v", result.Summary.DurationDeltaPct)
	}
}

// Removed span (in base, not in compare) — expect SpansRemoved=1 and status="removed".
func TestComputeDiff_RemovedSpan(t *testing.T) {
	baseSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch promo", "s2", "s1", 20_000_000, "{}"),
	}
	cmpSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
	}

	result := computeDiff(sess("a", "base"), sess("b", "cmp"), baseSpans, cmpSpans)

	if result.Summary.SpansRemoved != 1 {
		t.Errorf("SpansRemoved: want 1, got %d", result.Summary.SpansRemoved)
	}
	removed := findDiffSpan(t, result, "api", "fetch promo")
	if removed.Status != "removed" {
		t.Errorf("status: want removed, got %q", removed.Status)
	}
}

// Small (<5%) delta should be marked "unchanged" — exercises the threshold.
func TestComputeDiff_SmallDeltaUnchanged(t *testing.T) {
	baseSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch user", "s2", "s1", 100_000_000, "{}"),
	}
	cmpSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "fetch user", "s2", "s1", 102_000_000, "{}"), // +2%
	}

	result := computeDiff(sess("a", "base"), sess("b", "cmp"), baseSpans, cmpSpans)

	user := findDiffSpan(t, result, "api", "fetch user")
	if user.Status != "unchanged" {
		t.Errorf("status: want unchanged for sub-5%% delta, got %q (delta=%v)", user.Status, user.DeltaPct)
	}
}

// DB call delta — compare has more DB-tagged spans than base.
func TestComputeDiff_DBCallDelta(t *testing.T) {
	baseSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "db.query", "s2", "s1", 10_000_000, `{"db.system":"postgresql"}`),
	}
	cmpSpans := []*storage.Span{
		rootSpan("api", "GET /cart", "s1", 100_000_000),
		span("api", "db.query", "s2", "s1", 10_000_000, `{"db.system":"postgresql"}`),
		span("api", "db.query2", "s3", "s1", 8_000_000, `{"db.statement":"SELECT 1"}`),
	}

	result := computeDiff(sess("a", "base"), sess("b", "cmp"), baseSpans, cmpSpans)

	if result.Baseline.DBCalls != 1 {
		t.Errorf("baseline DBCalls: want 1, got %d", result.Baseline.DBCalls)
	}
	if result.Compare.DBCalls != 2 {
		t.Errorf("compare DBCalls: want 2, got %d", result.Compare.DBCalls)
	}
	if result.Summary.DBCallDelta != 1 {
		t.Errorf("DBCallDelta: want 1, got %d", result.Summary.DBCallDelta)
	}
}
