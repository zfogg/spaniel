package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
)

// TestMigrateFreshDB checks a brand-new file DB gets the schema, records the
// init migration, and stores the spaniel version in the meta table.
func TestMigrateFreshDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	if err := db.SetSpanielVersion("1.2.3"); err != nil {
		t.Fatalf("SetSpanielVersion: %v", err)
	}

	if _, err := db.CreateSession("s1", false); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got := db.SpanielVersion(); got != "1.2.3" {
		t.Fatalf("SpanielVersion = %q, want 1.2.3", got)
	}

	var applied int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM migrations WHERE id = '0001_init'`).Scan(&applied).Error; err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected 0001_init applied, got %d rows", applied)
	}
	db.Close()
}

// TestMigrateLegacyDB simulates a pre-gormigrate database: the spaniel tables
// already exist but there is no migrations/meta table. Opening it must stamp
// the init migration idempotently without clobbering existing data.
func TestMigrateLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.duckdb")

	// Build a "legacy" DB directly via the raw driver: just the spans table
	// with one row, no migrations/meta bookkeeping.
	raw, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE spans (
		trace_id TEXT, span_id TEXT, parent_span_id TEXT, service_name TEXT,
		name TEXT, kind INTEGER, start_ns BIGINT, end_ns BIGINT,
		duration_ns BIGINT GENERATED ALWAYS AS (end_ns - start_ns),
		status_code INTEGER, status_message TEXT, attributes JSON, resource JSON,
		session_id TEXT, session_label TEXT, received_at BIGINT)`); err != nil {
		t.Fatalf("legacy create: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO spans (trace_id, span_id, attributes, resource) VALUES ('t1','s1','{}','{}')`); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}
	raw.Close()

	// Open through the migration system — must not error and must preserve data.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	defer db.Close()

	spans, err := db.GetTrace("t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if len(spans) != 1 || spans[0].SpanID != "s1" {
		t.Fatalf("legacy data lost: got %+v", spans)
	}

	// The new tables must now exist.
	var n int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM meta`).Scan(&n).Error; err != nil {
		t.Fatalf("meta table missing after migrate: %v", err)
	}

	// A pre-gormigrate DB created its spans table with JSON columns and reaches
	// the schema via InitSchema (so migration 0005 is stamped but never run).
	// The init DDL's idempotent ALTERs must still have converted the columns to
	// VARCHAR — otherwise the Appender would double-encode attributes. Verify a
	// round-trip through the batched write path returns the JSON verbatim.
	if err := db.AppendSpan(&Span{TraceID: "t2", SpanID: "s2", Attributes: `{"k":"v"}`, Resource: `{}`}); err != nil {
		t.Fatalf("AppendSpan after legacy migrate: %v", err)
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("FlushBatch: %v", err)
	}
	got, err := db.GetTrace("t2")
	if err != nil || len(got) != 1 {
		t.Fatalf("GetTrace(t2): err=%v rows=%d", err, len(got))
	}
	if got[0].Attributes != `{"k":"v"}` {
		t.Fatalf("legacy JSON column not converted to VARCHAR: attributes=%q (want verbatim, not double-encoded)", got[0].Attributes)
	}
}
