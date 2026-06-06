package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func openFileDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "q.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.CreateSession("s", false); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := d.InsertSpan(&Span{
		TraceID: "t", SpanID: "a", ServiceName: "svc", Name: "n",
		StartNs: 1, EndNs: 2, SessionID: "s", ReceivedAt: 1,
	}); err != nil {
		t.Fatalf("insert span: %v", err)
	}
	_ = d.FlushBatch()
	return d
}

func spanCount(t *testing.T, d *DB) int {
	t.Helper()
	_, rows, _, err := d.ReadOnlyQuery(context.Background(), "SELECT count(*) FROM spans", 10)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	switch v := rows[0][0].(type) {
	case int64:
		return int(v)
	case int32:
		return int(v)
	default:
		t.Fatalf("unexpected count type %T", rows[0][0])
		return -1
	}
}

func TestReadOnlyQuery_Select(t *testing.T) {
	d := openFileDB(t)
	cols, rows, truncated, err := d.ReadOnlyQuery(context.Background(), "SELECT span_id, service_name FROM spans", 10)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(cols) != 2 || cols[0] != "span_id" {
		t.Errorf("columns = %v", cols)
	}
	if len(rows) != 1 || rows[0][0] != "a" || rows[0][1] != "svc" {
		t.Errorf("rows = %v", rows)
	}
	if truncated {
		t.Error("unexpected truncation")
	}
}

// DuckDB-specific read syntax (:: casts, json funcs) must work — this is why we
// don't pre-parse with a MySQL-dialect parser.
func TestReadOnlyQuery_DuckDBSyntax(t *testing.T) {
	d := openFileDB(t)
	_, rows, _, err := d.ReadOnlyQuery(context.Background(),
		"WITH x AS (SELECT span_id::VARCHAR AS s FROM spans) SELECT s FROM x", 10)
	if err != nil {
		t.Fatalf("duckdb syntax query: %v", err)
	}
	if len(rows) != 1 || rows[0][0] != "a" {
		t.Errorf("rows = %v", rows)
	}
}

// The engine — not us — rejects writes; data must be unchanged afterward.
func TestReadOnlyQuery_RejectsWrites(t *testing.T) {
	d := openFileDB(t)
	for _, q := range []string{
		"DELETE FROM spans",
		"INSERT INTO spans (trace_id, span_id) VALUES ('x','y')",
		"UPDATE spans SET name = 'z'",
		"DROP TABLE spans",
		"CREATE TABLE t (a INTEGER)",
	} {
		if _, _, _, err := d.ReadOnlyQuery(context.Background(), q, 10); err == nil {
			t.Errorf("expected engine to reject: %s", q)
		}
	}
	if n := spanCount(t, d); n != 1 {
		t.Errorf("span count changed to %d — a write leaked through read-only mode", n)
	}
}

func TestReadOnlyQuery_Truncation(t *testing.T) {
	d := openFileDB(t)
	// values(1),(2),(3) → 3 rows; cap at 2.
	_, rows, truncated, err := d.ReadOnlyQuery(context.Background(),
		"SELECT * FROM (VALUES (1),(2),(3)) AS v(n)", 2)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 2 || !truncated {
		t.Errorf("rows=%d truncated=%v, want 2 rows truncated", len(rows), truncated)
	}
}

func TestReadOnlyQuery_InMemoryUnsupported(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, _, err := d.ReadOnlyQuery(context.Background(), "SELECT 1", 10); err == nil {
		t.Error("expected in-memory DB to report read-only SQL unavailable")
	}
}
