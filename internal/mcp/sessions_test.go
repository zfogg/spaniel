package mcp

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// seedSessionSpans inserts a root + child span for a session so diff has data.
func seedSessionSpans(t *testing.T, store *storage.DB, sessID, traceID string, childDurNs int64) {
	t.Helper()
	root := &storage.Span{
		TraceID: traceID, SpanID: traceID + "-root", ServiceName: "api", Name: "GET /cart",
		StartNs: 0, EndNs: 100_000_000, StatusCode: 1,
		SessionID: sessID, SessionLabel: sessID, ReceivedAt: 1,
	}
	child := &storage.Span{
		TraceID: traceID, SpanID: traceID + "-db", ParentSpanID: traceID + "-root",
		ServiceName: "postgres", Name: "SELECT users",
		StartNs: 1_000_000, EndNs: 1_000_000 + childDurNs, StatusCode: 1,
		Attributes: `{"db.system":"postgresql"}`,
		SessionID:  sessID, SessionLabel: sessID, ReceivedAt: 1,
	}
	if err := store.InsertSpan(root); err != nil {
		t.Fatalf("insert root: %v", err)
	}
	if err := store.InsertSpan(child); err != nil {
		t.Fatalf("insert child: %v", err)
	}
}

func TestListSessions(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	// connect() already created and activated "test-session".
	active := store.ActiveSessionID()
	other, err := store.CreateSession("baseline-run", true)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	out := callStructured[ListSessionsOutput](t, cs, "list_sessions", nil)
	if out.ActiveSessionID != active {
		t.Errorf("active_session_id = %q, want %q", out.ActiveSessionID, active)
	}
	if out.Count < 2 {
		t.Fatalf("expected >=2 sessions, got %d", out.Count)
	}
	byID := map[string]SessionOut{}
	for _, s := range out.Sessions {
		byID[s.ID] = s
	}
	if !byID[active].IsActive {
		t.Errorf("active session not flagged active: %+v", byID[active])
	}
	if byID[other.ID].IsActive {
		t.Errorf("non-active session flagged active")
	}
	if !byID[other.ID].IsBaseline {
		t.Errorf("baseline session not flagged baseline")
	}
}

func TestDiffSessions(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})

	base, err := store.CreateSession("base", true)
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	cmp, err := store.CreateSession("cmp", false)
	if err != nil {
		t.Fatalf("create cmp: %v", err)
	}
	// Same shape; the DB span is 2x slower in compare → "changed" + positive delta.
	seedSessionSpans(t, store, base.ID, "tb", 20_000_000)
	seedSessionSpans(t, store, cmp.ID, "tc", 40_000_000)

	out := callStructured[DiffSessionsOutput](t, cs, "diff_sessions", map[string]any{
		"baseline": base.ID,
		"compare":  cmp.ID,
	})

	if out.Baseline.SessionID != base.ID || out.Compare.SessionID != cmp.ID {
		t.Errorf("sides wrong: %+v / %+v", out.Baseline, out.Compare)
	}
	if out.Baseline.DBCalls != 1 || out.Compare.DBCalls != 1 {
		t.Errorf("db calls: base=%d cmp=%d, want 1/1", out.Baseline.DBCalls, out.Compare.DBCalls)
	}
	// Find the SELECT users delta: 20ms → 40ms = +100%.
	var found bool
	for _, s := range out.Spans {
		if s.Name == "SELECT users" {
			found = true
			if s.Status != "changed" {
				t.Errorf("SELECT users status = %q, want changed", s.Status)
			}
			if s.DeltaPct != 100.0 {
				t.Errorf("SELECT users delta = %v, want 100", s.DeltaPct)
			}
		}
	}
	if !found {
		t.Errorf("no SELECT users delta in %+v", out.Spans)
	}
}

func TestDiffSessions_NotFound(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	base, _ := store.CreateSession("base", true)
	if !callToolIsError(t, cs, "diff_sessions", map[string]any{"baseline": base.ID, "compare": "nope"}) {
		t.Error("expected error for missing compare session")
	}
	if !callToolIsError(t, cs, "diff_sessions", map[string]any{"baseline": base.ID}) {
		t.Error("expected error when compare omitted")
	}
}

func TestSessionToolsRegistered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	names := toolNames(t, cs)
	for _, want := range []string{"list_sessions", "diff_sessions"} {
		if !names[want] {
			t.Errorf("tool %q not registered; have %v", want, names)
		}
	}
}
