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
	insertLintWarning(t, store, "sess", "http.missing_method", "no http method", "warning")
	if err := store.UpsertTraceIssue(&storage.TraceIssue{
		ID:            "trace-n1-1",
		TraceID:       "trace-n1",
		SessionID:     "sess",
		Kind:          "n_plus_one",
		Fingerprint:   "SELECT * FROM users WHERE id = ?",
		Count:         42,
		WastedNs:      12_500_000,
		ParentSpanID:  "parent-1",
		ExampleSpanID: "span-example",
		CreatedAt:     time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertTraceIssue: %v", err)
	}

	w := do(t, handler, http.MethodGet, "/api/lint", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	warnings := decodeData[[]storage.LintWarning](t, w.Body.Bytes())
	if len(warnings) != 2 {
		t.Fatalf("expected lint warning + N+1 issue (2), got %d: %+v", len(warnings), warnings)
	}
	var n1 *storage.LintWarning
	for i := range warnings {
		if warnings[i].RuleID == "n_plus_one" {
			n1 = &warnings[i]
		}
	}
	if n1 == nil {
		t.Fatalf("n_plus_one not surfaced on lint page: %+v", warnings)
	}
	if n1.Severity != "warning" || n1.SpanID != "span-example" || n1.TraceID != "trace-n1" {
		t.Errorf("n+1 warning fields wrong: %+v", n1)
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
