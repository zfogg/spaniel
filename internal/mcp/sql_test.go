package mcp

import (
	"fmt"
	"testing"
)

func TestQuerySQL_Select(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[QuerySQLOutput](t, cs, "query_sql", map[string]any{
		"sql": "SELECT count(*) AS c FROM spans",
	})
	if out.RowCount != 1 {
		t.Fatalf("expected 1 row, got %d", out.RowCount)
	}
	if got := fmt.Sprint(out.Rows[0][0]); got != "2" {
		t.Errorf("count = %s, want 2", got)
	}
	if len(out.Columns) != 1 || out.Columns[0] != "c" {
		t.Errorf("columns = %v, want [c]", out.Columns)
	}
}

func TestQuerySQL_GroupBy(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	out := callStructured[QuerySQLOutput](t, cs, "query_sql", map[string]any{
		"sql": "SELECT service_name, count(*) AS n FROM spans GROUP BY service_name ORDER BY service_name",
	})
	if out.RowCount != 2 {
		t.Fatalf("expected 2 rows, got %d (%v)", out.RowCount, out.Rows)
	}
	if fmt.Sprint(out.Rows[0][0]) != "api" || fmt.Sprint(out.Rows[1][0]) != "postgres" {
		t.Errorf("services = %v", out.Rows)
	}
}

// Multiple statements are allowed (no single-statement limit).
func TestQuerySQL_MultiStatement(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	if callToolIsError(t, cs, "query_sql", map[string]any{
		"sql": "SELECT 1 AS a; SELECT count(*) AS c FROM spans",
	}) {
		t.Error("multi-statement read query was rejected")
	}
}

// Writes are rejected by the DuckDB engine, and nothing is mutated.
func TestQuerySQL_RejectsWrite(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	seedTrace(t, store, store.ActiveSessionID())

	if !callToolIsError(t, cs, "query_sql", map[string]any{"sql": "DELETE FROM spans"}) {
		t.Error("expected DELETE to be rejected")
	}
	out := callStructured[QuerySQLOutput](t, cs, "query_sql", map[string]any{
		"sql": "SELECT count(*) FROM spans",
	})
	if got := fmt.Sprint(out.Rows[0][0]); got != "2" {
		t.Errorf("after rejected DELETE, count = %s, want 2", got)
	}
}

func TestQuerySQL_RequiresSQL(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	if !callToolIsError(t, cs, "query_sql", map[string]any{"sql": "  "}) {
		t.Error("expected error for empty sql")
	}
}

func TestQuerySQL_Registered(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	if !toolNames(t, cs)["query_sql"] {
		t.Error("query_sql not registered")
	}
}
