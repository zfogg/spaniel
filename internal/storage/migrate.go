package storage

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// execMigrationFile runs every statement in an embedded .sql file individually.
// The driver's multi-statement Exec only surfaces the last statement's error,
// so a mid-file failure would otherwise be silently masked; splitting makes
// every statement's error propagate.
func execMigrationFile(tx *gorm.DB, name string) error {
	b, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		// Embedded at build time — a missing file is a programming error.
		panic("storage: missing embedded migration " + name + ": " + err.Error())
	}
	for _, stmt := range splitSQLStatements(string(b)) {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// splitSQLStatements splits a SQL file into individual statements on top-level
// semicolons, ignoring semicolons inside single-quoted string literals, line
// comments (-- … EOL), and block comments (/* … */). Comments are stripped from
// the emitted statements so a comment-only fragment never reaches the driver as
// an "empty query" — a naive Split(s, ";") breaks on a semicolon anywhere in a
// comment or literal. Whitespace/comment-only statements are dropped.
func splitSQLStatements(sql string) []string {
	const (
		normal = iota
		lineComment
		blockComment
		inString
	)
	var (
		stmts []string
		buf   strings.Builder
		state = normal
		rs    = []rune(sql)
	)
	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			stmts = append(stmts, s)
		}
		buf.Reset()
	}
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch state {
		case normal:
			switch {
			case c == '-' && i+1 < len(rs) && rs[i+1] == '-':
				state, i = lineComment, i+1
			case c == '/' && i+1 < len(rs) && rs[i+1] == '*':
				state, i = blockComment, i+1
			case c == '\'':
				state = inString
				buf.WriteRune(c)
			case c == ';':
				flush()
			default:
				buf.WriteRune(c)
			}
		case lineComment:
			if c == '\n' {
				state = normal
				buf.WriteRune(c) // preserve line breaks between statements
			}
		case blockComment:
			if c == '*' && i+1 < len(rs) && rs[i+1] == '/' {
				state, i = normal, i+1
			}
		case inString:
			buf.WriteRune(c)
			if c == '\'' {
				if i+1 < len(rs) && rs[i+1] == '\'' { // '' is an escaped quote
					buf.WriteRune(rs[i+1])
					i++
				} else {
					state = normal
				}
			}
		}
	}
	flush()
	return stmts
}

// migrations is the ordered list of schema migrations. Append new entries here
// (never edit or reorder applied ones); gormigrate records applied IDs in the
// `migrations` table, which is the effective schema version.
func migrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{
			ID: "0001_init",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0001_init.sql")
			},
		},
		{
			ID: "0002_session_note_activity",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0002_session_note_activity.sql")
			},
		},
		{
			ID: "0003_sampled",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0003_sampled.sql")
			},
		},
		{
			ID: "0004_metrics_exemplars",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0004_metrics_exemplars.sql")
			},
		},
		{
			ID: "0005_json_to_varchar",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0005_json_to_varchar.sql")
			},
		},
		{
			ID: "0006_metrics_exemplars_repair",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0006_metrics_exemplars_repair.sql")
			},
		},
		{
			ID: "0007_performance_indexes",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0007_performance_indexes.sql")
			},
		},
		{
			ID: "0008_materialize_span_duration",
			Migrate: func(tx *gorm.DB) error {
				return execMigrationFile(tx, "0008_materialize_span_duration.sql")
			},
		},
		{
			ID:      "0009_dashboards_alerts",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0009_dashboards_alerts.sql") },
		},
		{
			ID:      "0010_alert_notification_state",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0010_alert_notification_state.sql") },
		},
		{
			ID:      "0011_sql_dashboard_queries",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0011_sql_dashboard_queries.sql") },
		},
		{
			ID:      "0012_remove_spaniel_dashboard_variables",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0012_remove_spaniel_dashboard_variables.sql") },
		},
		{
			ID:      "0013_remove_legacy_dashboard_queries",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0013_remove_legacy_dashboard_queries.sql") },
		},
		{
			ID:      "0014_lossless_metric_points",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0014_lossless_metric_points.sql") },
		},
		{
			ID:      "0015_metric_scope_attributes",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0015_metric_scope_attributes.sql") },
		},
		{
			ID:      "0016_reorder_metric_scope_attributes",
			Migrate: func(tx *gorm.DB) error { return execMigrationFile(tx, "0016_reorder_metric_scope_attributes.sql") },
		},
	}
}

// migrate runs all pending schema migrations. For a fresh (or pre-gormigrate)
// database, InitSchema applies the current full schema in one shot and stamps
// every known migration as applied. The init DDL is idempotent (IF NOT EXISTS),
// so it is safe to run against an existing user database created before this
// system was introduced.
func (d *DB) migrate() error {
	// Pre-create gormigrate's tracking table ourselves. gormigrate otherwise
	// builds it via GORM AutoMigrate, and this DuckDB driver's CreateTable
	// panics on an "id" primary-key column (unchecked clause.Table assertion).
	// With the table already present, gormigrate's HasTable check skips that
	// path entirely and only uses plain INSERT/SELECT against it.
	if err := d.gorm.Exec(
		`CREATE TABLE IF NOT EXISTS migrations (id VARCHAR PRIMARY KEY)`,
	).Error; err != nil {
		return err
	}

	m := gormigrate.New(d.gorm, gormigrate.DefaultOptions, migrations())
	m.InitSchema(func(tx *gorm.DB) error {
		if err := execMigrationFile(tx, "0001_init.sql"); err != nil {
			return err
		}
		// Fresh databases are stamped with every migration, so new definition
		// tables must be included here as well as in their numbered migration.
		if err := execMigrationFile(tx, "0009_dashboards_alerts.sql"); err != nil {
			return err
		}
		// InitSchema stamps every migration as applied. Include schema changes
		// introduced after the dashboards migration, or a fresh database would
		// be marked current while missing them.
		if err := execMigrationFile(tx, "0010_alert_notification_state.sql"); err != nil {
			return err
		}
		if err := execMigrationFile(tx, "0011_sql_dashboard_queries.sql"); err != nil {
			return err
		}
		if err := execMigrationFile(tx, "0012_remove_spaniel_dashboard_variables.sql"); err != nil {
			return err
		}
		if err := execMigrationFile(tx, "0013_remove_legacy_dashboard_queries.sql"); err != nil {
			return err
		}
		return execMigrationFile(tx, "0014_lossless_metric_points.sql")
	})
	if err := m.Migrate(); err != nil {
		return err
	}
	// gormigrate's InitSchema marks every numbered migration as applied. A
	// pre-gormigrate database therefore may retain the old generated duration
	// column while 0008 is already stamped. Inspect the effective schema and
	// repair it explicitly, just as 0001 already repairs legacy JSON columns.
	var tableSQL string
	if err := d.gorm.Raw(`
		SELECT sql FROM duckdb_tables()
		WHERE table_name = 'spans' AND internal = FALSE
	`).Scan(&tableSQL).Error; err != nil {
		return err
	}
	if strings.Contains(strings.ToUpper(tableSQL), "DURATION_NS BIGINT GENERATED") {
		return execMigrationFile(d.gorm, "0008_materialize_span_duration.sql")
	}
	return nil
}

// backfillLegacyDashboardSQL is a one-time upgrade for dashboards and alerts
// saved before SQL became the public query language. It consumes only the
// already-persisted, server-produced AST JSON; no legacy parser remains in the
// runtime query path.
func (d *DB) backfillLegacyDashboardSQL() error {
	var legacyColumns int64
	if err := d.gorm.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'dashboard_panels' AND column_name = 'query_json'`).Scan(&legacyColumns).Error; err != nil {
		return err
	}
	if legacyColumns == 0 {
		return nil
	}
	type filter struct{ Field, Op, Value string }
	type legacy struct {
		Function, Signal, Target, GroupBy string
		Filters                           []filter
	}
	compile := func(raw, display string) string {
		var q legacy
		if json.Unmarshal([]byte(raw), &q) != nil || q.Function == "" {
			return ""
		}
		table, timeColumn := "telemetry_spans", "start_ns"
		if q.Signal == "logs" {
			table, timeColumn = "telemetry_logs", "timestamp_ns"
		}
		if q.Signal == "metrics" {
			table, timeColumn = "telemetry_metrics", "timestamp_ns"
		}
		if q.Signal == "traces" {
			table = "telemetry_traces"
		}
		if q.Function == "records" {
			return "SELECT * FROM " + table + " LIMIT 100"
		}
		field := legacySQLField(q.Target)
		agg := map[string]string{"count": "count(*)", "rate": "count(*) / greatest((max(" + timeColumn + ") - min(" + timeColumn + ")) / 1000000000.0, 1)", "last": "max(" + field + ")", "sum": "sum(" + field + ")", "avg": "avg(" + field + ")", "p50": "quantile_cont(" + field + ", 0.5)", "p95": "quantile_cont(" + field + ", 0.95)", "p99": "quantile_cont(" + field + ", 0.99)", "heatmap": "count(*)"}[q.Function]
		if agg == "" {
			return ""
		}
		where := ""
		for _, f := range q.Filters {
			value := f.Value
			if strings.HasPrefix(value, "$") {
				value = f.Value
			} else {
				value = "'" + strings.ReplaceAll(value, "'", "''") + "'"
			}
			where += " AND " + legacySQLField(f.Field) + " " + f.Op + " " + value
		}
		if where != "" {
			where = " WHERE " + strings.TrimPrefix(where, " AND ")
		}
		if display == "time_series" {
			bucket := "date_trunc('minute', make_timestamp_ns(" + timeColumn + "))"
			selectSQL := bucket + " AS timestamp, " + agg + " AS value"
			group := bucket
			if q.GroupBy != "" {
				g := legacySQLField(q.GroupBy)
				selectSQL = bucket + " AS timestamp, " + g + " AS series, " + agg + " AS value"
				group += ", " + g
			}
			return "SELECT " + selectSQL + " FROM " + table + where + " GROUP BY " + group + " ORDER BY timestamp"
		}
		if q.GroupBy != "" {
			g := legacySQLField(q.GroupBy)
			return "SELECT " + g + " AS group_value, " + agg + " AS value FROM " + table + where + " GROUP BY " + g + " ORDER BY value DESC"
		}
		return "SELECT " + agg + " AS value FROM " + table + where
	}
	type panel struct{ ID, QueryJSON, DisplayType string }
	var panels []panel
	if err := d.gorm.Raw("SELECT id, query_json, display_type FROM dashboard_panels WHERE query_sql = ''").Scan(&panels).Error; err != nil {
		return err
	}
	for _, p := range panels {
		sql := compile(p.QueryJSON, p.DisplayType)
		if sql == "" {
			return fmt.Errorf("migrate dashboard panel %s: unsupported legacy query", p.ID)
		}
		if err := d.gorm.Exec("UPDATE dashboard_panels SET query_sql = ?, query_version = 1 WHERE id = ?", sql, p.ID).Error; err != nil {
			return err
		}
	}
	type rule struct{ ID, QueryJSON string }
	var rules []rule
	if err := d.gorm.Raw("SELECT id, query_json FROM alert_rules WHERE query_sql = ''").Scan(&rules).Error; err != nil {
		return err
	}
	for _, r := range rules {
		sql := compile(r.QueryJSON, "")
		if sql == "" {
			return fmt.Errorf("migrate alert rule %s: unsupported legacy query", r.ID)
		}
		if err := d.gorm.Exec("UPDATE alert_rules SET query_sql = ?, query_version = 1 WHERE id = ?", sql, r.ID).Error; err != nil {
			return err
		}
	}
	// DuckDB 1.1 cannot drop any column from a table while an index exists,
	// even if that index does not reference the column being removed.
	if err := d.gorm.Exec("DROP INDEX IF EXISTS idx_dashboard_panels_order").Error; err != nil {
		return err
	}
	for _, statement := range []string{
		"ALTER TABLE dashboard_panels DROP COLUMN query_text",
		"ALTER TABLE dashboard_panels DROP COLUMN query_json",
		"ALTER TABLE alert_rules DROP COLUMN query_json",
	} {
		if err := d.gorm.Exec(statement).Error; err != nil {
			return err
		}
	}
	return d.gorm.Exec("CREATE INDEX IF NOT EXISTS idx_dashboard_panels_order ON dashboard_panels(dashboard_id, position)").Error
}

func legacySQLField(field string) string {
	switch field {
	case "service", "service_name":
		return "service_name"
	case "name":
		return "name"
	case "duration":
		return "duration_ns"
	case "status":
		return "status_code"
	case "severity":
		return "severity"
	case "body", "trace_id", "span_id", "value":
		return field
	}
	if strings.HasPrefix(field, "attributes.") {
		return "json_extract_string(attributes, '$." + strings.TrimPrefix(field, "attributes.") + "')"
	}
	if strings.HasPrefix(field, "resource.") {
		return "json_extract_string(resource, '$." + strings.TrimPrefix(field, "resource.") + "')"
	}
	return fmt.Sprintf("%q", field)
}

// SetSpanielVersion records the running binary version in the meta table. It is
// informational (surfaced in doctor/settings); schema versioning is owned by the
// migrations table. Best-effort by design — callers may ignore the error.
func (d *DB) SetSpanielVersion(version string) error {
	return d.gorm.Exec(
		`INSERT OR REPLACE INTO meta (meta_key, meta_value) VALUES ('spaniel_version', ?)`,
		version,
	).Error
}

// SpanielVersion returns the spaniel version recorded in the meta table, or "".
func (d *DB) SpanielVersion() string {
	var v string
	_ = d.gorm.Raw(`SELECT meta_value FROM meta WHERE meta_key = 'spaniel_version'`).Scan(&v).Error
	return v
}
