package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func insertLintWarning(t *testing.T, store *storage.DB, sessID, ruleID, msg, severity string) {
	t.Helper()
	if err := store.InsertLintWarning(&storage.LintWarning{
		SpanID:    "span-" + ruleID,
		TraceID:   "trace-" + ruleID,
		SessionID: sessID,
		RuleID:    ruleID,
		Message:   msg,
		Severity:  severity,
		CreatedAt: time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("InsertLintWarning: %v", err)
	}
}

func TestListLint_WithWarnings(t *testing.T) {
	handler, store := setupRouter(t)
	insertLintWarning(t, store, "sess", "http.missing_method", "no http method", "warning")
	insertLintWarning(t, store, "sess", "zero_duration", "span has zero duration", "info")

	w := do(t, handler, http.MethodGet, "/api/lint", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	warnings := decodeData[[]storage.LintWarning](t, w.Body.Bytes())
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}
	// Verify every required field round-trips.
	seenRules := map[string]bool{}
	for _, lw := range warnings {
		if lw.SpanID == "" || lw.TraceID == "" || lw.Severity == "" || lw.Message == "" {
			t.Errorf("warning missing fields: %+v", lw)
		}
		if lw.CreatedAt == 0 {
			t.Errorf("CreatedAt is zero: %+v", lw)
		}
		seenRules[lw.RuleID] = true
	}
	if !seenRules["http.missing_method"] || !seenRules["zero_duration"] {
		t.Errorf("missing rules in response: %+v", seenRules)
	}
}

func TestListLint_IncludesN1Issues(t *testing.T) {
	handler, store := setupRouter(t)

	// A semantic-convention lint warning.
	insertLintWarning(t, store, "sess-1", "http.missing_method", "no http method", "warning")

	// An N+1 detector finding stored in trace_issues — must also appear in /api/lint.
	if err := store.UpsertTraceIssue(&storage.TraceIssue{
		ID:            "issue-n1",
		TraceID:       "trace-abc",
		SessionID:     "sess-1",
		Kind:          "n_plus_one",
		Fingerprint:   "SELECT * FROM orders WHERE id = ?",
		Count:         8,
		WastedNs:      4_000_000,
		ExampleSpanID: "span-db-1",
		CreatedAt:     time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertTraceIssue: %v", err)
	}

	w := do(t, handler, http.MethodGet, "/api/lint?sessionId=sess-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	items := decodeData[[]storage.LintWarning](t, w.Body.Bytes())

	ruleIDs := map[string]bool{}
	for _, item := range items {
		ruleIDs[item.RuleID] = true
	}
	if !ruleIDs["http.missing_method"] {
		t.Errorf("lint warning missing from /api/lint: %+v", ruleIDs)
	}
	if !ruleIDs["n_plus_one"] {
		t.Errorf("N+1 trace issue missing from /api/lint: %+v", ruleIDs)
	}

	// The N+1 row must have the session scoped correctly.
	for _, item := range items {
		if item.RuleID == "n_plus_one" && item.SessionID != "sess-1" {
			t.Errorf("N+1 row has wrong session_id: %q", item.SessionID)
		}
	}
}

func TestListLint_SessionFilter(t *testing.T) {
	handler, store := setupRouter(t)
	insertLintWarning(t, store, "sess-A", "rule-1", "msg-a", "warning")
	insertLintWarning(t, store, "sess-B", "rule-2", "msg-b", "warning")

	w := do(t, handler, http.MethodGet, "/api/lint?sessionId=sess-B", nil)
	warnings := decodeData[[]storage.LintWarning](t, w.Body.Bytes())
	if len(warnings) != 1 || warnings[0].SessionID != "sess-B" || warnings[0].RuleID != "rule-2" {
		t.Errorf("session filter wrong: %+v", warnings)
	}
}
