package storage

import (
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertAndGetTrace(t *testing.T) {
	db := openTestDB(t)

	traceID := "trace-abc-123"
	spans := []*Span{
		{
			TraceID:     traceID,
			SpanID:      "span-1",
			ServiceName: "svc-a",
			Name:        "root",
			Kind:        1,
			StartNs:     1000,
			EndNs:       2000,
			Attributes:  "{}",
			Resource:    "{}",
			ReceivedAt:  time.Now().UnixNano(),
		},
		{
			TraceID:      traceID,
			SpanID:       "span-2",
			ParentSpanID: "span-1",
			ServiceName:  "svc-a",
			Name:         "child-a",
			Kind:         3,
			StartNs:      1100,
			EndNs:        1500,
			Attributes:   "{}",
			Resource:     "{}",
			ReceivedAt:   time.Now().UnixNano(),
		},
		{
			TraceID:      traceID,
			SpanID:       "span-3",
			ParentSpanID: "span-1",
			ServiceName:  "svc-b",
			Name:         "child-b",
			Kind:         4,
			StartNs:      1200,
			EndNs:        1900,
			Attributes:   "{}",
			Resource:     "{}",
			ReceivedAt:   time.Now().UnixNano(),
		},
	}

	for _, s := range spans {
		if err := db.InsertSpan(s); err != nil {
			t.Fatalf("InsertSpan(%q) failed: %v", s.SpanID, err)
		}
	}

	got, err := db.GetTrace(traceID)
	if err != nil {
		t.Fatalf("GetTrace failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 spans, got %d", len(got))
	}

	// Verify all span IDs are present.
	seen := map[string]bool{}
	for _, s := range got {
		seen[s.SpanID] = true
	}
	for _, s := range spans {
		if !seen[s.SpanID] {
			t.Errorf("span %q not returned by GetTrace", s.SpanID)
		}
	}
}

func TestUpsertTraceIssue(t *testing.T) {
	db := openTestDB(t)

	issue := &TraceIssue{
		ID:            "issue-1",
		TraceID:       "trace-xyz",
		SessionID:     "sess-1",
		Kind:          "n_plus_one",
		Fingerprint:   "SELECT * FROM t WHERE id = ?",
		Count:         10,
		WastedNs:      500000,
		ParentSpanID:  "parent-span",
		ExampleSpanID: "example-span",
		CreatedAt:     time.Now().UnixNano(),
	}

	if err := db.UpsertTraceIssue(issue); err != nil {
		t.Fatalf("UpsertTraceIssue failed: %v", err)
	}

	got, err := db.GetTraceIssues("trace-xyz")
	if err != nil {
		t.Fatalf("GetTraceIssues failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(got))
	}
	if got[0].Count != 10 {
		t.Errorf("expected count=10, got %d", got[0].Count)
	}
	if got[0].Kind != "n_plus_one" {
		t.Errorf("expected kind=n_plus_one, got %q", got[0].Kind)
	}

	// Upsert with updated count.
	issue.Count = 25
	if err := db.UpsertTraceIssue(issue); err != nil {
		t.Fatalf("UpsertTraceIssue (update) failed: %v", err)
	}

	got2, err := db.GetTraceIssues("trace-xyz")
	if err != nil {
		t.Fatalf("GetTraceIssues (after update) failed: %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("expected 1 issue after upsert, got %d", len(got2))
	}
	if got2[0].Count != 25 {
		t.Errorf("expected updated count=25, got %d", got2[0].Count)
	}
}

func TestListTracesHasN1(t *testing.T) {
	db := openTestDB(t)

	traceWithIssue := "trace-has-n1"
	traceClean := "trace-clean"

	// Insert a root span for the trace with N+1 (parent_span_id = '' so it appears as root).
	if err := db.InsertSpan(&Span{
		TraceID:     traceWithIssue,
		SpanID:      "root-1",
		ServiceName: "svc",
		Name:        "root",
		Kind:        1,
		StartNs:     1000,
		EndNs:       9000,
		Attributes:  "{}",
		Resource:    "{}",
		ReceivedAt:  time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("InsertSpan failed: %v", err)
	}

	// Insert a root span for the clean trace.
	if err := db.InsertSpan(&Span{
		TraceID:     traceClean,
		SpanID:      "root-2",
		ServiceName: "svc",
		Name:        "root",
		Kind:        1,
		StartNs:     2000,
		EndNs:       3000,
		Attributes:  "{}",
		Resource:    "{}",
		ReceivedAt:  time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("InsertSpan failed: %v", err)
	}

	// Insert a TraceIssue for the first trace only.
	if err := db.UpsertTraceIssue(&TraceIssue{
		ID:            "issue-n1",
		TraceID:       traceWithIssue,
		SessionID:     "",
		Kind:          "n_plus_one",
		Fingerprint:   "SELECT * FROM t WHERE id = ?",
		Count:         15,
		WastedNs:      100000,
		ParentSpanID:  "root-1",
		ExampleSpanID: "root-1",
		CreatedAt:     time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertTraceIssue failed: %v", err)
	}

	traces, err := db.ListTraces(TraceFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListTraces failed: %v", err)
	}

	found := map[string]*TraceRow{}
	for _, tr := range traces {
		found[tr.TraceID] = tr
	}

	if tr, ok := found[traceWithIssue]; !ok {
		t.Errorf("trace %q not in ListTraces results", traceWithIssue)
	} else if !tr.HasN1 {
		t.Errorf("expected has_n1=true for trace %q", traceWithIssue)
	}

	if tr, ok := found[traceClean]; !ok {
		t.Errorf("trace %q not in ListTraces results", traceClean)
	} else if tr.HasN1 {
		t.Errorf("expected has_n1=false for trace %q", traceClean)
	}
}

func TestListLintWarnings(t *testing.T) {
	db := openTestDB(t)

	w := &LintWarning{
		SpanID:    "span-lint-1",
		TraceID:   "trace-lint-1",
		SessionID: "sess-lint",
		RuleID:    "unknown_service",
		Message:   "service.name is 'unknown_service'",
		Severity:  "warning",
		CreatedAt: time.Now().UnixNano(),
	}

	if err := db.InsertLintWarning(w); err != nil {
		t.Fatalf("InsertLintWarning failed: %v", err)
	}

	// Query with no session filter.
	warnings, err := db.ListLintWarnings("")
	if err != nil {
		t.Fatalf("ListLintWarnings failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].RuleID != "unknown_service" {
		t.Errorf("expected rule_id=unknown_service, got %q", warnings[0].RuleID)
	}
	if warnings[0].SpanID != "span-lint-1" {
		t.Errorf("expected span_id=span-lint-1, got %q", warnings[0].SpanID)
	}

	// Query with matching session filter.
	warnings2, err := db.ListLintWarnings("sess-lint")
	if err != nil {
		t.Fatalf("ListLintWarnings(sess-lint) failed: %v", err)
	}
	if len(warnings2) != 1 {
		t.Fatalf("expected 1 warning with session filter, got %d", len(warnings2))
	}

	// Query with non-matching session filter.
	warnings3, err := db.ListLintWarnings("other-session")
	if err != nil {
		t.Fatalf("ListLintWarnings(other-session) failed: %v", err)
	}
	if len(warnings3) != 0 {
		t.Errorf("expected 0 warnings for non-matching session, got %d", len(warnings3))
	}
}
