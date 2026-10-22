package mcp

import (
	"testing"
)

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

func TestListLintWarnings_RuleSeverityAndTimeFilters(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	if got := callStructured[ListLintOutput](t, cs, "list_lint_warnings", map[string]any{"rule_id": "db.system_required", "severity": "warn", "from_ns": 1, "to_ns": 1}); got.Count != 1 {
		t.Fatalf("matching lint filters returned %d warnings: %+v", got.Count, got.Warnings)
	}
	if got := callStructured[ListLintOutput](t, cs, "list_lint_warnings", map[string]any{"severity": "error"}); got.Count != 0 {
		t.Errorf("unmatched severity returned %d warnings", got.Count)
	}
}

// list_lint_warnings must be available without --mcp-allow-writes.
func TestDiagnosticToolsRegistered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	names := toolNames(t, cs)
	if !names["list_lint_warnings"] {
		t.Errorf("tool %q not registered; have %v", "list_lint_warnings", names)
	}
	if names["list_issues"] {
		t.Error("list_issues must not expose a second public findings shape")
	}
}
