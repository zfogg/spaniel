package storage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// rebuildTelemetry removes deleted tuples and rebuilds their ART indexes. In
// the bundled DuckDB, CHECKPOINT cannot compact indexed row groups and VACUUM
// does not reclaim deletes. Retention must reclaim those rows before deciding
// that more live telemetry needs eviction. The SQL pool and appenders must be
// exclusively held by withMaintenance for the entire operation.
func (d *DB) rebuildTelemetry() error {
	var tables []struct{ TableName, SQL string }
	if err := d.namedGORM("ReclaimTableSchemas").Raw(`
		SELECT table_name, sql FROM duckdb_tables()
		WHERE schema_name = 'main' AND NOT temporary
		ORDER BY table_name`).Scan(&tables).Error; err != nil {
		return err
	}
	var indexes []struct{ TableName, SQL string }
	if err := d.namedGORM("ReclaimIndexSchemas").Raw(`
		SELECT table_name, sql FROM duckdb_indexes()
		WHERE schema_name = 'main' AND database_name = current_database()`).Scan(&indexes).Error; err != nil {
		return err
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	for _, table := range tables {
		// Avoid copying healthy tables on each guard pass. estimated_size counts
		// physical tuples, including deleted rows, on this DuckDB version.
		var counts struct{ Stored, Live int64 }
		if err := d.namedGORM("ReclaimRowCounts").Raw(`SELECT
			(SELECT estimated_size FROM duckdb_tables() WHERE table_name = ? AND schema_name = 'main' AND NOT temporary) AS stored,
			(SELECT COUNT(*) FROM `+quote(table.TableName)+`) AS live`, table.TableName).Scan(&counts).Error; err != nil {
			return err
		}
		if counts.Stored <= counts.Live {
			continue
		}
		if err := d.namedGORM("ReclaimDeletedRows").Transaction(func(tx *gorm.DB) error {
			for _, sql := range []string{
				`CREATE TEMP TABLE retention_live_copy AS SELECT * FROM ` + quote(table.TableName),
				`DROP TABLE ` + quote(table.TableName),
				table.SQL,
				`INSERT INTO ` + quote(table.TableName) + ` SELECT * FROM retention_live_copy`,
			} {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
			}
			for _, index := range indexes {
				if index.TableName == table.TableName {
					if err := tx.Exec(index.SQL).Error; err != nil {
						return err
					}
				}
			}
			return tx.Exec(`DROP TABLE retention_live_copy`).Error
		}); err != nil {
			return fmt.Errorf("rebuild %s: %w", table.TableName, err)
		}
	}
	return d.checkpointWithRetry()
}
