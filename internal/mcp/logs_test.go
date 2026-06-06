package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestQueryLogs_Default(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[QueryLogsOutput](t, cs, "query_logs", nil)
	if out.Count != 1 {
		t.Fatalf("expected 1 log, got %d", out.Count)
	}
	l := out.Logs[0]
	if l.Severity != "ERROR" {
		t.Errorf("severity = %q, want ERROR", l.Severity)
	}
	if l.Body != "query failed" {
		t.Errorf("body = %q, want 'query failed'", l.Body)
	}
	if l.Service != "postgres" {
		t.Errorf("service = %q, want postgres", l.Service)
	}
}

func TestQueryLogs_MinSeverity(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	// Seeded log is ERROR (17).
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"min_severity": "WARN"}); got.Count != 1 {
		t.Errorf("min_severity=WARN: expected 1, got %d", got.Count)
	}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"min_severity": "ERROR"}); got.Count != 1 {
		t.Errorf("min_severity=ERROR: expected 1, got %d", got.Count)
	}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"min_severity": "FATAL"}); got.Count != 0 {
		t.Errorf("min_severity=FATAL: expected 0, got %d", got.Count)
	}
}

func TestQueryLogs_Filters(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"service": "postgres"}); got.Count != 1 {
		t.Errorf("service=postgres: expected 1, got %d", got.Count)
	}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"service": "api"}); got.Count != 0 {
		t.Errorf("service=api: expected 0, got %d", got.Count)
	}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"trace_id": testTraceID}); got.Count != 1 {
		t.Errorf("trace filter (match): expected 1, got %d", got.Count)
	}
	if got := callStructured[QueryLogsOutput](t, cs, "query_logs", map[string]any{"trace_id": "nope"}); got.Count != 0 {
		t.Errorf("trace filter (miss): expected 0, got %d", got.Count)
	}
}

func TestSearch_FreeText(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[SearchOutput](t, cs, "search", map[string]any{"query": "users"})
	if out.Count == 0 {
		t.Fatal("expected search hits for 'users'")
	}
	found := false
	for _, r := range out.Results {
		if r.TraceID == testTraceID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a result referencing %s, got %+v", testTraceID, out.Results)
	}
}

func TestSearch_LintFilter(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	lint := callStructured[SearchOutput](t, cs, "search", map[string]any{"query": "lint:db.system_required"})
	if lint.Count == 0 {
		t.Errorf("lint:db.system_required: expected >=1 result")
	}
	// The n+1 alias is a lint rule value: "lint:n+1" → n_plus_one detector hits.
	n1 := callStructured[SearchOutput](t, cs, "search", map[string]any{"query": "lint:n+1"})
	if n1.Count == 0 {
		t.Errorf("lint:n+1: expected >=1 result")
	}
}

func TestSearch_RequiresQuery(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "search", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error when query is empty")
	}
}

func TestLogSearchToolsRegistered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	names := toolNames(t, cs)
	for _, want := range []string{"query_logs", "search"} {
		if !names[want] {
			t.Errorf("tool %q not registered; have %v", want, names)
		}
	}
}
