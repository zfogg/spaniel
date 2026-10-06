package storage

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
)

// TestSplitSQLStatements covers the cases a naive Split(s, ";") gets wrong:
// semicolons inside comments and string literals, and comment-only fragments.
func TestSplitSQLStatements(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "semicolon in line comment is not a separator",
			sql:  "-- drop this; and that\nCREATE TABLE t (a INT);",
			want: []string{"CREATE TABLE t (a INT)"},
		},
		{
			name: "semicolon in string literal is preserved",
			sql:  "INSERT INTO t VALUES ('a;b');\nINSERT INTO t VALUES ('c');",
			want: []string{"INSERT INTO t VALUES ('a;b')", "INSERT INTO t VALUES ('c')"},
		},
		{
			name: "escaped quote inside string",
			sql:  "INSERT INTO t VALUES ('it''s; ok');",
			want: []string{"INSERT INTO t VALUES ('it''s; ok')"},
		},
		{
			name: "block comment with semicolons dropped",
			sql:  "/* a; b; c */ ALTER TABLE t ADD COLUMN x INT;",
			want: []string{"ALTER TABLE t ADD COLUMN x INT"},
		},
		{
			name: "comment-only file yields no statements",
			sql:  "-- nothing here;\n/* or here; */\n",
			want: nil,
		},
		{
			name: "multiple statements",
			sql:  "CREATE TABLE a (i INT);\nCREATE TABLE b (j INT);",
			want: []string{"CREATE TABLE a (i INT)", "CREATE TABLE b (j INT)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSQLStatements(tc.sql)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitSQLStatements()\n got = %#v\nwant = %#v", got, tc.want)
			}
		})
	}
}

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
	// InitSchema records 0010 as applied, so it must also have applied its
	// DDL. This catches fresh databases that silently miss notification state.
	var notificationColumn int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'alert_instances' AND column_name = 'last_notified_at'`).Scan(&notificationColumn).Error; err != nil {
		t.Fatalf("inspect alert_instances columns: %v", err)
	}
	if notificationColumn != 1 {
		t.Fatalf("last_notified_at column missing from fresh alert_instances schema")
	}
	var viewCount int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM duckdb_views() WHERE view_name = 'telemetry_spans'`).Scan(&viewCount).Error; err != nil {
		t.Fatalf("inspect telemetry views: %v", err)
	}
	if viewCount != 1 {
		t.Fatalf("telemetry_spans view missing from fresh schema")
	}
	var legacyColumns int64
	if err := db.gorm.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'dashboard_panels' AND column_name IN ('query_text', 'query_json')`).Scan(&legacyColumns).Error; err != nil {
		t.Fatalf("inspect legacy query columns: %v", err)
	}
	if legacyColumns != 0 {
		t.Fatalf("legacy query DSL columns remain: %d", legacyColumns)
	}
	db.Close()
}

// TestMigrateAddsMetricScopeAttributesToAlreadyAppliedLosslessSchema covers
// databases that applied the original 0014 schema before scope_attributes was
// added to the metric write path.  Their 0014 record prevents that migration
// from running again, so the following migration must make the Appender's
// 37-value rows valid without rebuilding metric data.
func TestMigrateAddsMetricScopeAttributesToAlreadyAppliedLosslessSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metric-scope-attributes.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	for _, index := range []string{
		"idx_metric_points_lookup",
		"idx_metric_points_series",
		"idx_metric_points_global_lookup",
		"idx_metric_points_name_time",
	} {
		if err := db.gorm.Exec(`DROP INDEX ` + index).Error; err != nil {
			t.Fatalf("drop %s before simulating original 0014: %v", index, err)
		}
	}
	if err := db.gorm.Exec(`ALTER TABLE metrics DROP COLUMN scope_attributes`).Error; err != nil {
		t.Fatalf("remove scope_attributes to simulate original 0014: %v", err)
	}
	if err := db.gorm.Exec(`DELETE FROM migrations WHERE id IN ('0015_metric_scope_attributes', '0016_reorder_metric_scope_attributes')`).Error; err != nil {
		t.Fatalf("unapply scope-attributes migrations: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close simulated old schema: %v", err)
	}
	closed = true

	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open migrated schema: %v", err)
	}
	defer db.Close()
	if err := db.AppendMetric(&Metric{Name: "test.metric", Type: "gauge", TimestampNs: 1, ScopeAttributes: `{"scope":"value"}`}); err != nil {
		t.Fatalf("AppendMetric after scope-attributes migration: %v", err)
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("FlushBatch after scope-attributes migration: %v", err)
	}
	var stored string
	if err := db.gorm.Raw(`SELECT scope_attributes FROM metrics WHERE name = 'test.metric'`).Scan(&stored).Error; err != nil {
		t.Fatalf("read stored scope_attributes: %v", err)
	}
	if stored != `{"scope":"value"}` {
		t.Fatalf("scope_attributes = %q, want preserved value", stored)
	}
}

func TestMigrateRemovesSpanielDashboardVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard-variables.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO dashboard_variables (dashboard_id, name, kind, source, options_json, default_value) VALUES ('d', 'spaniel.db.latency', 'string', 'spaniel.db.latency', '[]', '')`,
		`INSERT INTO dashboard_variables (dashboard_id, name, kind, source, options_json, default_value) VALUES ('d', 'service', 'string', 'telemetry_spans.service_name', '[]', '')`,
		`DELETE FROM migrations WHERE id = '0012_remove_spaniel_dashboard_variables'`,
	} {
		if err := db.gorm.Exec(statement).Error; err != nil {
			t.Fatalf("prepare migration (%q): %v", statement, err)
		}
	}
	db.Close()

	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer db.Close()
	var names []string
	if err := db.gorm.Raw(`SELECT name FROM dashboard_variables ORDER BY name`).Scan(&names).Error; err != nil {
		t.Fatalf("read dashboard variables: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"service"}) {
		t.Fatalf("dashboard variables after migration = %#v, want only service", names)
	}
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
