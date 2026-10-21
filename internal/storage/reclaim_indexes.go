package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const reclaimIndexJournalTable = "retention_index_journal"

type reclaimIndex struct {
	TableName, IndexName, SQL string
	IsUnique                  bool
}
type reclaimDefault struct{ ColumnName, ColumnDefault string }
type reclaimIndexPlan struct {
	Table, Staging string
	Indexes        []reclaimIndex
}

func reclaimStagingTable(table string) string { return "spaniel_retention_copy_" + table }
func reclaimQuote(name string) string         { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func reclaimCopySQL(table, staging string) string {
	// On DuckDB 1.1.3, parallel BATCH_CREATE_TABLE_AS can leave each worker's
	// final full row group unpersisted and serialize the entire copy into WAL.
	// This all-rows materializing source selects ordinary CREATE_TABLE_AS and
	// persists full row groups before commit, without changing global settings.
	return "CREATE TABLE " + reclaimQuote(staging) + " AS SELECT * FROM " + reclaimQuote(table) + " LIMIT 100 PERCENT"
}

func marshalReclaimIndexPlan(plan reclaimIndexPlan) (string, error) {
	sort.Slice(plan.Indexes, func(i, j int) bool { return plan.Indexes[i].IndexName < plan.Indexes[j].IndexName })
	data, err := json.Marshal(plan)
	return string(data), err
}

func writeReclaimIndexPlan(tx *gorm.DB, plan reclaimIndexPlan, active bool) error {
	var records []struct {
		PlanJSON string
		Active   bool
	}
	if err := tx.Raw("SELECT plan_json,active FROM retention_index_journal WHERE table_name=?", plan.Table).Scan(&records).Error; err != nil {
		return err
	}
	if len(records) > 1 || (len(records) == 1 && records[0].Active) {
		return fmt.Errorf("reclamation index plan already pending for %q", plan.Table)
	}
	data, err := marshalReclaimIndexPlan(plan)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return tx.Exec("INSERT INTO retention_index_journal(table_name,plan_json,active) VALUES (?,?,?)", plan.Table, data, active).Error
	}
	if records[0].PlanJSON != data {
		if err := tx.Exec("UPDATE retention_index_journal SET plan_json=? WHERE table_name=? AND NOT active", data, plan.Table).Error; err != nil {
			return err
		}
	}
	return tx.Exec("UPDATE retention_index_journal SET active=? WHERE table_name=? AND NOT active", active, plan.Table).Error
}

// Seed immutable definitions before callers measure their storage budget. Only
// the inline BOOLEAN changes during ordinary maintenance; no JSON tombstones.
func (d *DB) initReclaimIndexSlots() error {
	var indexes []reclaimIndex
	if err := d.gorm.Raw("SELECT table_name,index_name,sql,is_unique FROM duckdb_indexes() WHERE schema_name='main'").Scan(&indexes).Error; err != nil {
		return err
	}
	return d.maintenanceTransaction(d.gorm, func(tx *gorm.DB) error {
		for _, table := range retentionReclaimTables {
			plan := reclaimIndexPlan{Table: table, Staging: reclaimStagingTable(table)}
			for _, index := range indexes {
				if index.TableName == table {
					plan.Indexes = append(plan.Indexes, index)
				}
			}
			if err := writeReclaimIndexPlan(tx, plan, false); err != nil {
				return err
			}
		}
		return nil
	})
}

// The journal and index drops commit together. Rows, defaults and constraints
// remain in their original table throughout native vacuum and crash recovery.
func (d *DB) saveAndDropReclaimIndexes(plan reclaimIndexPlan) error {
	for _, index := range plan.Indexes {
		if index.IsUnique {
			return fmt.Errorf("refuse to suspend unique index %q", index.IndexName)
		}
	}
	return d.maintenanceTransaction(d.namedGORM("SuspendReclaimIndexes"), func(tx *gorm.DB) error {
		// A pending plan is authoritative even if its indexes are currently
		// absent from the catalog. Never overwrite it with a fresh snapshot.
		if err := writeReclaimIndexPlan(tx, plan, true); err != nil {
			return err
		}
		for _, index := range plan.Indexes {
			if err := tx.Exec("DROP INDEX " + reclaimQuote(index.IndexName)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func restoreReclaimIndexPlan(tx *gorm.DB, plan reclaimIndexPlan) error {
	var exists int64
	if err := tx.Raw("SELECT count(*) FROM duckdb_tables() WHERE table_name=? AND schema_name='main'", plan.Table).Scan(&exists).Error; err != nil {
		return err
	}
	if exists != 1 {
		return fmt.Errorf("cannot recover reclamation: canonical table %q is missing", plan.Table)
	}
	if err := tx.Exec("DROP TABLE IF EXISTS " + reclaimQuote(plan.Staging)).Error; err != nil {
		return err
	}
	for _, index := range plan.Indexes {
		if err := tx.Raw("SELECT count(*) FROM duckdb_indexes() WHERE index_name=? AND schema_name='main'", index.IndexName).Scan(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			if err := tx.Exec(index.SQL).Error; err != nil {
				return err
			}
		}
	}
	return tx.Exec("UPDATE retention_index_journal SET active=false WHERE table_name=?", plan.Table).Error
}

// Called before migrations/appenders on Open and while maintenance owns every
// connection. Never discard a staging copy if the canonical table is missing.
func (d *DB) recoverReclaimIndexes() error {
	var exists int64
	if err := d.gorm.Raw("SELECT count(*) FROM duckdb_tables() WHERE table_name=? AND schema_name='main'", reclaimIndexJournalTable).Scan(&exists).Error; err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var records []struct{ TableName, PlanJSON string }
	if err := d.namedGORM("PendingReclaimIndexes").Raw("SELECT table_name,plan_json FROM retention_index_journal WHERE active ORDER BY table_name").Scan(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		var plan reclaimIndexPlan
		if err := json.Unmarshal([]byte(record.PlanJSON), &plan); err != nil {
			return fmt.Errorf("decode reclaim index plan: %w", err)
		}
		if plan.Table == "" || record.TableName != plan.Table || plan.Staging != reclaimStagingTable(plan.Table) {
			return fmt.Errorf("invalid reclaim index plan %q", record.TableName)
		}
		if err := d.maintenanceTransaction(d.namedGORM("RecoverReclaimIndexes"), func(tx *gorm.DB) error { return restoreReclaimIndexPlan(tx, plan) }); err != nil {
			return err
		}
	}
	if len(records) > 0 {
		return d.checkpointWithRetry()
	}
	return nil
}

// DuckDB 1.1 cannot vacuum indexed row groups. Temporarily removing nonunique
// indexes lets its native checkpoint reclaim committed rows without copying
// the whole live table into an indexed transaction's enormous logical WAL.
func (d *DB) reclaimUnconstrainedTable(table string, indexes []reclaimIndex, defaults []reclaimDefault) (err error) {
	var before struct{ Groups, Blocks int64 }
	if err = d.namedGORM("ReclaimPhysicalGroups").Raw("SELECT count(DISTINCT row_group_id) AS groups,count(DISTINCT block_id) FILTER (WHERE block_id>=0) AS blocks FROM pragma_storage_info(?)", table).Scan(&before).Error; err != nil {
		return err
	}
	plan := reclaimIndexPlan{Table: table, Staging: reclaimStagingTable(table)}
	for _, index := range indexes {
		if index.TableName == table {
			plan.Indexes = append(plan.Indexes, index)
		}
	}
	if err = d.saveAndDropReclaimIndexes(plan); err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			// Restoration is required even after the optional work deadline. It
			// uses the already reserved maintenance connection, never pool capacity.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(d.gorm.Statement.Context), 8*time.Second)
			defer cancel()
			cleanup := d.WithContext(ctx)
			recoveryErr := cleanup.recoverReclaimIndexes()
			if recoveryErr == nil {
				recoveryErr = cleanup.checkpointWithRetry()
			}
			err = errors.Join(err, recoveryErr)
		}
	}()
	if err = d.checkpointWithRetry(); err != nil {
		return err
	}
	var after struct{ Blocks int64 }
	if err = d.namedGORM("ReclaimVacuumBlocks").Raw("SELECT count(DISTINCT block_id) FILTER (WHERE block_id>=0) AS blocks FROM pragma_storage_info(?)", table).Scan(&after).Error; err != nil {
		return err
	}
	if before.Groups <= 1 || after.Blocks >= before.Blocks {
		// A single sparse row group cannot merge with a neighbour. Commit its
		// copy even if estimated_size was reduced without freeing any blocks.
		// unindexed copy first: full row groups use persisted block references.
		// Only then swap names/defaults/indexes atomically, with no inserted
		// payload in that indexed transaction. The durable journal cleans an
		// abandoned copy and restores the canonical indexes after a crash.
		if err = d.namedGORM("ReclaimUnindexedCopy").Exec(reclaimCopySQL(table, plan.Staging)).Error; err != nil {
			return err
		}
		err = d.maintenanceTransaction(d.namedGORM("ReclaimAtomicSwap"), func(tx *gorm.DB) error {
			if err := tx.Exec("DROP TABLE " + reclaimQuote(table)).Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE " + reclaimQuote(plan.Staging) + " RENAME TO " + reclaimQuote(table)).Error; err != nil {
				return err
			}
			for _, column := range defaults {
				if err := tx.Exec("ALTER TABLE " + reclaimQuote(table) + " ALTER COLUMN " + reclaimQuote(column.ColumnName) + " SET DEFAULT " + column.ColumnDefault).Error; err != nil {
					return err
				}
			}
			return restoreReclaimIndexPlan(tx, plan)
		})
	} else {
		err = d.maintenanceTransaction(d.namedGORM("RestoreReclaimIndexes"), func(tx *gorm.DB) error { return restoreReclaimIndexPlan(tx, plan) })
	}
	if err != nil {
		return err
	}
	err = d.checkpointWithRetry()
	finished = err == nil
	return err
}
