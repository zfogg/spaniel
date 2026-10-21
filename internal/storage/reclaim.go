package storage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Only these tables participate in oldest-prefix telemetry eviction. A fixed
// cycle prevents frequently updated metadata from starving a large span rebuild.
var retentionReclaimTables = [...]string{"lint_warnings", "logs", "metrics", "sessions", "span_events", "span_links", "spans", "trace_issues"}

func retentionReclaimBit(table string) uint64 {
	for i, name := range retentionReclaimTables {
		if name == table {
			return uint64(1) << i
		}
	}
	return 0
}

// rebuildTelemetry removes deleted tuples and rebuilds their ART indexes. In
// the bundled DuckDB, CHECKPOINT cannot compact indexed row groups and VACUUM
// does not reclaim deletes. Retention must reclaim those rows before deciding
// that more live telemetry needs eviction. The SQL pool and appenders must be
// exclusively held by withMaintenance for the entire operation.
func (d *DB) rebuildTelemetry(progress ...*reclaimProgress) error {
	var tables []struct{ TableName, SQL string }
	if err := d.namedGORM("ReclaimTableSchemas").Raw(`
		SELECT table_name, sql FROM duckdb_tables()
		WHERE schema_name = 'main' AND NOT temporary
		ORDER BY table_name`).Scan(&tables).Error; err != nil {
		return err
	}
	var indexes []reclaimIndex
	if err := d.namedGORM("ReclaimIndexSchemas").Raw(`
		SELECT table_name, index_name, sql, is_unique FROM duckdb_indexes()
		WHERE schema_name = 'main' AND database_name = current_database()`).Scan(&indexes).Error; err != nil {
		return err
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	for _, table := range tables {
		if table.TableName == reclaimIndexJournalTable {
			continue
		}
		var bit uint64
		bounded := len(progress) > 0 && progress[0].OneTable
		if bounded {
			bit = retentionReclaimBit(table.TableName)
			if bit == 0 || progress[0].Visited&bit != 0 {
				continue
			}
		}
		// Avoid copying healthy tables on each guard pass. estimated_size counts
		// physical tuples, including deleted rows, on this DuckDB version.
		var counts struct{ Stored, Live int64 }
		if err := d.namedGORM("ReclaimRowCounts").Raw(`SELECT
			(SELECT estimated_size FROM duckdb_tables() WHERE table_name = ? AND schema_name = 'main' AND NOT temporary) AS stored,
			(SELECT COUNT(*) FROM `+quote(table.TableName)+`) AS live`, table.TableName).Scan(&counts).Error; err != nil {
			return err
		}
		if counts.Stored <= counts.Live {
			if bounded {
				progress[0].Visited |= bit
			}
			continue
		}
		switch table.TableName {
		case "spans", "logs", "metrics", "span_events", "span_links":
			if len(progress) > 0 {
				progress[0].HadTelemetryDeletes = true
				// Protect an initial cycle too: preexisting tombstones whose
				// reclamation cannot free blocks must not cause fresh live loss.
				if bounded && d.retentionFloor.Load() == 0 {
					d.retentionFloor.Store(progress[0].BeforeRebuild)
				}
			}
		}
		if len(progress) > 0 && progress[0].OneTable && progress[0].Rebuilt {
			progress[0].Remaining = true
			continue
		}
		// Native vacuum requires no indexes. Unconstrained tables can drop their
		// nonunique indexes with durable recovery intent; sparse row groups use
		// a committed unindexed copy before an atomic schema/index swap.
		var constraints int64
		if err := d.namedGORM("ReclaimConstraints").Raw("SELECT count(*) FROM duckdb_constraints() WHERE table_name=? AND schema_name='main'", table.TableName).Scan(&constraints).Error; err != nil {
			return err
		}
		fast := constraints == 0
		for _, index := range indexes {
			if index.TableName == table.TableName && index.IsUnique {
				fast = false
			}
		}
		var defaults []reclaimDefault
		if fast {
			if err := d.namedGORM("ReclaimDefaults").Raw("SELECT column_name,column_default FROM duckdb_columns() WHERE table_name=? AND schema_name='main' AND column_default IS NOT NULL", table.TableName).Scan(&defaults).Error; err != nil {
				return err
			}
		}
		var rebuildErr error
		if fast {
			rebuildErr = d.reclaimUnconstrainedTable(table.TableName, indexes, defaults)
		} else {
			rebuildErr = d.maintenanceTransaction(d.namedGORM("ReclaimDeletedRows"), func(tx *gorm.DB) error {
				statements := []string{
					`CREATE TEMP TABLE retention_live_copy AS SELECT * FROM ` + quote(table.TableName),
					`DROP TABLE ` + quote(table.TableName),
					table.SQL,
					`INSERT INTO ` + quote(table.TableName) + ` SELECT * FROM retention_live_copy`,
				}
				for _, sql := range statements {
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
			})
		}
		if rebuildErr != nil {
			return fmt.Errorf("rebuild %s: %w", table.TableName, rebuildErr)
		}
		if len(progress) > 0 {
			progress[0].Rebuilt = true
			if bounded {
				progress[0].Visited |= bit
			}
		}
	}
	if len(progress) > 0 && progress[0].OneTable && !progress[0].Rebuilt {
		return nil
	}
	return d.checkpointWithRetry()
}
