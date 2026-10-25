package mcp

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestQuerySQL_SpanNames(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	cs, _ := connect(t, Options{Version: "t"})
	for _, tc := range []struct{ name, want string }{
		{"", "storage.duckdb.UserQuery"},
		{" \t", "storage.duckdb.UserQuery"},
		{"Custom_Query1", "storage.duckdb.Custom_Query1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(recorder.Ended())
			callStructured[QuerySQLOutput](t, cs, "query_sql", map[string]any{"sql": "SELECT 1", "name": tc.name})
			for _, span := range recorder.Ended()[before:] {
				if span.Name() == tc.want {
					return
				}
			}
			t.Fatalf("no %s span recorded", tc.want)
		})
	}
}

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

func TestQuerySQL_WritePermissions(t *testing.T) {
	for _, allowTools := range []bool{false, true} {
		for _, allowSQL := range []bool{false, true} {
			t.Run(fmt.Sprintf("tools=%t/sql=%t", allowTools, allowSQL), func(t *testing.T) {
				cs, store := connect(t, Options{AllowWrites: allowTools, AllowSQLWrites: allowSQL})
				seedTrace(t, store, store.ActiveSessionID())
				info := callStructured[ServerInfo](t, cs, "get_server_info", nil)
				if info.WritesEnabled != allowTools || info.SQLWritesEnabled != allowSQL {
					t.Fatalf("reported permissions: tools=%t SQL=%t, want tools=%t SQL=%t", info.WritesEnabled, info.SQLWritesEnabled, allowTools, allowSQL)
				}
				if got := toolNames(t, cs)["create_session"]; got != allowTools {
					t.Fatalf("create_session = %t", got)
				}
				tools, err := cs.ListTools(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, tool := range tools.Tools {
					if tool.Name == "query_sql" && tool.Annotations.ReadOnlyHint == allowSQL {
						t.Fatal("incorrect SQL read-only annotation")
					}
				}
				failed := callToolIsError(t, cs, "query_sql", map[string]any{"sql": "DELETE FROM spans"})
				if failed == allowSQL {
					t.Fatalf("DELETE failed = %t, SQL writes = %t", failed, allowSQL)
				}
				out := callStructured[QuerySQLOutput](t, cs, "query_sql", map[string]any{"sql": "SELECT count(*) FROM spans"})
				want := "2"
				if allowSQL {
					want = "0"
				}
				if got := fmt.Sprint(out.Rows[0][0]); got != want {
					t.Fatalf("count = %s, want %s", got, want)
				}
				if _, _, _, err := store.ReadOnlyQuery(context.Background(), "DELETE FROM spans", 1); err == nil {
					t.Fatal("read-only runner accepted write")
				}
			})
		}
	}
}
