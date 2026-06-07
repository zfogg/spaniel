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

// TestMigrateRepairsStaleMetrics reproduces the v0.1.x-born database shape:
// a metrics table missing the `exemplars` column even though 0004 is recorded
// as applied (gormigrate's InitSchema stamped every migration without running
// it). Migration 0006 must rebuild the table to the canonical 10-column order
// the Appender writes to, preserve existing rows, and unblock metric writes.
func TestMigrateRepairsStaleMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.duckdb")

	// Start from a correct DB, then corrupt it into the stale shape: drop the
	// 10-col metrics table, recreate the old 9-col one with a row, and forget
	// 0006 so it re-runs on reopen (0004 stays recorded, as on a real stale DB).
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE metrics`,
		`CREATE TABLE metrics (
			name TEXT, description TEXT, unit TEXT, type TEXT, timestamp_ns BIGINT,
			value DOUBLE, attributes VARCHAR, service_name TEXT, session_id TEXT)`,
		`INSERT INTO metrics VALUES
			('http.server.duration','','ms','histogram',1,1.5,'{}','svc-a','sess-1')`,
		`DELETE FROM migrations WHERE id = '0006_metrics_exemplars_repair'`,
	} {
		if err := db.gorm.Exec(stmt).Error; err != nil {
			t.Fatalf("corrupt to stale shape (%q): %v", stmt, err)
		}
	}
	db.Close()

	// Reopen: migration 0006 should run and repair the table.
	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open stale: %v", err)
	}
	defer db.Close()

	// exemplars must exist at ordinal 8 (the position AppendMetric binds it).
	type col struct {
		Name string `gorm:"column:column_name"`
		Pos  int    `gorm:"column:ordinal_position"`
	}
	var cols []col
	if err := db.gorm.Raw(
		`SELECT column_name, ordinal_position FROM information_schema.columns
		 WHERE table_name = 'metrics' ORDER BY ordinal_position`).Scan(&cols).Error; err != nil {
		t.Fatalf("introspect metrics: %v", err)
	}
	if len(cols) != 10 {
		t.Fatalf("metrics column count = %d, want 10: %+v", len(cols), cols)
	}
	if cols[7].Name != "exemplars" {
		t.Fatalf("exemplars at ordinal %d (%q), want position 8", cols[7].Pos, cols[7].Name)
	}

	// The pre-existing row must survive the rebuild.
	var preserved int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM metrics WHERE name = 'http.server.duration'`).Scan(&preserved).Error; err != nil {
		t.Fatalf("count preserved: %v", err)
	}
	if preserved != 1 {
		t.Fatalf("pre-existing metric row lost: count = %d", preserved)
	}

	// A new metric write through the Appender must now succeed (this is exactly
	// what failed before with "invalid column count: expected 9, got 10").
	if err := db.AppendMetric(&Metric{
		Name: "process.runtime.go.goroutines", Type: "gauge", Unit: "{goroutine}",
		TimestampNs: 2, Value: 12, Attributes: `{}`, Exemplars: `[]`,
		ServiceName: "svc-a", SessionID: "sess-1",
	}); err != nil {
		t.Fatalf("AppendMetric after repair: %v", err)
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("FlushBatch: %v", err)
	}
	var total int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM metrics`).Scan(&total).Error; err != nil {
		t.Fatalf("count metrics: %v", err)
	}
	if total != 2 {
		t.Fatalf("metrics count = %d, want 2 (1 preserved + 1 appended)", total)
	}
}
