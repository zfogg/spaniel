package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alifiroozi80/duckdb"
	"gorm.io/gorm"
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

// openQueryGateDB avoids migrations so mutation-regression coverage remains
// focused on the query gate even while schema migrations evolve independently.
func openQueryGateDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "query-gate.duckdb")
	g, err := gorm.Open(duckdb.Open(duckDBDSN(path, false)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open query gate database: %v", err)
	}
	d := &DB{gorm: g, path: path}
	if err := g.Exec(`CREATE TABLE spans (trace_id VARCHAR, span_id VARCHAR, name VARCHAR)`).Error; err != nil {
		t.Fatalf("create spans: %v", err)
	}
	if err := g.Exec(`INSERT INTO spans VALUES ('t', 'a', 'original')`).Error; err != nil {
		t.Fatalf("seed spans: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
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

func TestReadOnlyQueryArgs_BindsNamedValues(t *testing.T) {
	d := openFileDB(t)
	_, rows, _, err := d.ReadOnlyQueryArgs(context.Background(),
		"SELECT span_id FROM telemetry_spans WHERE service_name = $service", []any{sql.Named("service", "svc")}, 10)
	if err != nil {
		t.Fatalf("named argument query: %v", err)
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

// TestReadOnlyQuery_MutationRegression exercises the gate through the public
// query path. On Windows these statements would otherwise run on Spaniel's
// shared read-write DuckDB instance; on other platforms DuckDB's read-only
// instance provides a second line of defence.
func TestReadOnlyQuery_MutationRegression(t *testing.T) {
	d := openQueryGateDB(t)
	queries := []string{
		"ALTER TABLE spans RENAME TO spans_renamed",
		"ANALYZE spans",
		"ATTACH ':memory:' AS other",
		"BEGIN TRANSACTION",
		"CALL checkpoint()",
		"CHECKPOINT",
		"COMMENT ON TABLE spans IS 'changed'",
		"COMMIT",
		"COPY spans TO 'spans.csv'",
		"CREATE TABLE injected (id INTEGER)",
		"DELETE FROM spans",
		"DETACH other",
		"DROP TABLE spans",
		"EXPORT DATABASE 'exported-db'",
		"IMPORT DATABASE 'exported-db'",
		"INSERT INTO spans (trace_id, span_id) VALUES ('injected', 'injected')",
		"INSTALL httpfs",
		"LOAD httpfs",
		"MERGE INTO spans USING spans AS source ON false WHEN MATCHED THEN DELETE",
		"PRAGMA enable_profiling",
		"REINDEX spans",
		"ROLLBACK",
		"SET threads = 1",
		"TRUNCATE spans",
		"UPDATE spans SET name = 'changed'",
		"VACUUM",
		"/* leading comment */ DeLeTe FROM spans",
		"WITH changed AS (DELETE FROM spans RETURNING *) SELECT * FROM changed",
		"SELECT 1; DROP TABLE spans",
		"EXPLAIN UPDATE spans SET name = 'changed'",
	}

	for _, query := range queries {
		t.Run(strings.Fields(query)[0], func(t *testing.T) {
			if _, _, _, err := d.ReadOnlyQuery(context.Background(), query, 10); err == nil {
				t.Fatalf("mutation was accepted: %q", query)
			}
			var count int64
			if err := d.SQL().QueryRowContext(context.Background(), "SELECT count(*) FROM spans").Scan(&count); err != nil {
				t.Fatalf("count spans after %q: %v", query, err)
			}
			if count != 1 {
				t.Fatalf("span count after %q = %d, want 1", query, count)
			}
		})
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

func TestValidateReadOnlySQL(t *testing.T) {
	for _, query := range []string{
		"SELECT 'DELETE' AS verb",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- UPDATE is only a comment\nSELECT 1",
		"SELECT \"DROP\" FROM spans",
		"SELECT 1; -- one trailing terminator is still one statement",
	} {
		if err := validateReadOnlySQL(query); err != nil {
			t.Errorf("validateReadOnlySQL(%q): %v", query, err)
		}
	}

	for _, query := range []string{
		"DELETE FROM spans",
		"EXPLAIN UPDATE spans SET name = 'z'",
		"WITH changed AS (DELETE FROM spans RETURNING *) SELECT * FROM changed",
		"SELECT 1; DROP TABLE spans",
		"SELECT 1;;",
	} {
		err := validateReadOnlySQL(query)
		if err == nil {
			t.Errorf("validateReadOnlySQL(%q) unexpectedly succeeded", query)
		} else if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("validateReadOnlySQL(%q) = %v, want not-allowed error", query, err)
		}
	}
}
