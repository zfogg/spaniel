package mcp

import (
	"testing"
)

func TestListIssues_Session(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListIssuesOutput](t, cs, "list_issues", nil)
	if out.Count != 1 {
		t.Fatalf("expected 1 issue, got %d (%+v)", out.Count, out.Issues)
	}
	is := out.Issues[0]
	if is.Kind != "n_plus_one" {
		t.Errorf("kind = %q, want n_plus_one", is.Kind)
	}
	if is.Count != 5 {
		t.Errorf("count = %d, want 5", is.Count)
	}
	if is.WastedMs != 50.0 {
		t.Errorf("wasted_ms = %v, want 50", is.WastedMs)
	}
	if is.ExampleSpanID != dbSpanID {
		t.Errorf("example_span_id = %q, want %q", is.ExampleSpanID, dbSpanID)
	}
}

func TestListIssues_TraceScope(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListIssuesOutput](t, cs, "list_issues", map[string]any{"trace_id": testTraceID})
	if out.TraceID != testTraceID {
		t.Errorf("trace_id = %q, want %q", out.TraceID, testTraceID)
	}
	if out.Count != 1 || out.Issues[0].Kind != "n_plus_one" {
		t.Errorf("expected one n_plus_one issue, got %+v", out.Issues)
	}
}

func TestListIssues_Empty(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out := callStructured[ListIssuesOutput](t, cs, "list_issues", nil)
	if out.Count != 0 {
		t.Errorf("expected 0 issues on empty session, got %d", out.Count)
	}
}

func TestListLintWarnings(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[ListLintOutput](t, cs, "list_lint_warnings", nil)
	// One seeded semconv warning + the n+1 detector finding projected as lint.
	if out.Count < 2 {
		t.Fatalf("expected >=2 warnings, got %d (%+v)", out.Count, out.Warnings)
	}
	var foundSemconv, foundDetector bool
	for _, w := range out.Warnings {
		if w.TraceID != testTraceID {
			t.Errorf("warning has trace_id %q, want %q", w.TraceID, testTraceID)
		}
		switch w.RuleID {
		case "db.system_required":
			foundSemconv = true
		case "n_plus_one":
			foundDetector = true
		}
	}
	if !foundSemconv {
		t.Error("missing seeded db.system_required warning")
	}
	if !foundDetector {
		t.Error("missing n_plus_one detector finding projected as lint")
	}
}

func TestListLintWarnings_TraceFilter(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	hit := callStructured[ListLintOutput](t, cs, "list_lint_warnings", map[string]any{"trace_id": testTraceID})
	if hit.Count < 1 {
		t.Errorf("trace filter (match): expected >=1, got %d", hit.Count)
	}
	miss := callStructured[ListLintOutput](t, cs, "list_lint_warnings", map[string]any{"trace_id": "no-such-trace"})
	if miss.Count != 0 {
		t.Errorf("trace filter (no match): expected 0, got %d", miss.Count)
	}
}

// list_issues and list_lint_warnings must be available without --mcp-allow-writes.
func TestDiagnosticToolsRegistered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	names := toolNames(t, cs)
	for _, want := range []string{"list_issues", "list_lint_warnings"} {
		if !names[want] {
			t.Errorf("tool %q not registered; have %v", want, names)
		}
	}
}
