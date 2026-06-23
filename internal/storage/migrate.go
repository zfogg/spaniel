package storage

import (
	"embed"
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
		return execMigrationFile(tx, "0001_init.sql")
	})
	return m.Migrate()
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
