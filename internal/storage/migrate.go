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
	for stmt := range strings.SplitSeq(string(b), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
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
