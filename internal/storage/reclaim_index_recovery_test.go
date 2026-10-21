package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReclaimIndexRecoveryAfterAbruptExit(t *testing.T) {
	const pathEnv = "SPANIEL_RECLAIM_INDEX_RECOVERY_PATH"
	const phaseEnv = "SPANIEL_RECLAIM_INDEX_RECOVERY_PHASE"
	type snapshot struct{ Schema, Rows, Indexes string }
	if path := os.Getenv(pathEnv); path != "" {
		phase := os.Getenv(phaseEnv)
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		// A committed database already on migration0040 cannot rely on migrations
		// to restore indexes removed by interrupted maintenance.
		seedSession(t, d, "crash-recovery", "crash-recovery", time.Now().UnixNano(), false, 8000)
		if err := d.gorm.Exec("CREATE TABLE unrelated_retention_stage AS SELECT 42 AS preserved").Error; err != nil {
			t.Fatal(err)
		}
		var before snapshot
		before.Schema, before.Rows, before.Indexes = reclaimSnapshot(t, d)
		encoded, err := json.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".snapshot", encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if err := d.withMaintenance(func(d *DB) error {
			if err := d.checkpointWithRetry(); err != nil {
				return err
			}
			var indexes []reclaimIndex
			if err := d.namedGORM("TestReclaimRecoveryIndexes").Raw("SELECT table_name,index_name,sql FROM duckdb_indexes() WHERE table_name='spans' ORDER BY index_name").Scan(&indexes).Error; err != nil {
				return err
			}
			if len(indexes) != 4 {
				return fmt.Errorf("expected four selective indexes, got %d", len(indexes))
			}
			plan := reclaimIndexPlan{Table: "spans", Staging: reclaimStagingTable("spans"), Indexes: indexes}
			if err := d.saveAndDropReclaimIndexes(plan); err != nil {
				return err
			}
			var remaining int64
			if err := d.namedGORM("TestReclaimRecoveryDroppedIndexes").Raw("SELECT count(*) FROM duckdb_indexes() WHERE table_name='spans'").Scan(&remaining).Error; err != nil {
				return err
			}
			if remaining != 0 {
				return fmt.Errorf("indexes not dropped: %d", remaining)
			}
			if phase != "after-drop" {
				if err := d.checkpointWithRetry(); err != nil {
					return err
				}
			}
			if phase == "after-stage" {
				if err := d.gorm.Exec(reclaimCopySQL("spans", plan.Staging)).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// No Close or defers: recovery must work with the committed WAL left by
		// this process, including the durable index plan and optional staging copy.
		os.Exit(0)
	}
	for _, phase := range []string{"after-drop", "after-checkpoint", "after-stage"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spaniel.duckdb")
			child := exec.Command(os.Args[0], "-test.run=^TestReclaimIndexRecoveryAfterAbruptExit$", "-test.count=1")
			child.Env = append(os.Environ(), pathEnv+"="+path, phaseEnv+"="+phase)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("crash fixture: %v\n%s", err, output)
			}
			encoded, err := os.ReadFile(path + ".snapshot")
			if err != nil {
				t.Fatal(err)
			}
			var before snapshot
			if err := json.Unmarshal(encoded, &before); err != nil {
				t.Fatal(err)
			}
			d, err := Open(path)
			if err != nil {
				t.Fatalf("startup recovery: %v", err)
			}
			defer func() { _ = d.Close() }()
			verify := func() {
				t.Helper()
				schema, rows, indexes := reclaimSnapshot(t, d)
				if schema != before.Schema || rows != before.Rows || indexes != before.Indexes {
					t.Fatalf("recovery changed schema=%v payload=%v indexes=%v", schema != before.Schema, rows != before.Rows, indexes != before.Indexes)
				}
				var stages int
				if err := d.SQL().QueryRow("SELECT count(*) FROM duckdb_tables() WHERE table_name=?", reclaimStagingTable("spans")).Scan(&stages); err != nil {
					t.Fatal(err)
				}
				if stages != 0 {
					t.Fatal("recovery left the planned staging table")
				}
				var plans int
				if err := d.SQL().QueryRow("SELECT count(*) FROM retention_index_journal WHERE active").Scan(&plans); err != nil {
					t.Fatal(err)
				}
				if plans != 0 {
					t.Fatal("recovery left an index restoration plan")
				}
				var preserved int
				if err := d.SQL().QueryRow("SELECT preserved FROM unrelated_retention_stage").Scan(&preserved); err != nil {
					t.Fatal(err)
				}
				if preserved != 42 {
					t.Fatal("unrelated staging data changed")
				}
			}
			verify()
			// A second restart must remain healthy after the index journal is cleared.
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			d, err = Open(path)
			if err != nil {
				t.Fatalf("second restart: %v", err)
			}
			verify()
			if err := d.gorm.Exec("INSERT INTO spans(trace_id,span_id) VALUES('restored-default','restored-default')").Error; err != nil {
				t.Fatal(err)
			}
			var sampled bool
			if err := d.SQL().QueryRow("SELECT sampled FROM spans WHERE span_id='restored-default'").Scan(&sampled); err != nil {
				t.Fatal(err)
			}
			if !sampled {
				t.Fatal("sampled default lost during recovery")
			}
		})
	}
}

// A failed index restore must leave its original recovery definitions intact;
// a retry's empty catalog snapshot cannot replace the durable journal.
func TestReclaimIndexPlanSurvivesFailedRestoreAndRetry(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "retry-recovery", "retry-recovery", time.Now().UnixNano(), false, 128)
	schema, rows, indexesBefore := reclaimSnapshot(t, d)
	forced := errors.New("injected index restoration failure")
	failRestore := true
	const callback = "test_reclaim_index_restore_failure"
	d.gorm.Callback().Raw().After("gorm:raw").Register(callback, func(tx *gorm.DB) {
		if failRestore && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String())), "CREATE INDEX") {
			tx.AddError(forced)
		}
	})
	defer d.gorm.Callback().Raw().Remove(callback)
	if err := d.withMaintenance(func(d *DB) error {
		var indexes []reclaimIndex
		if err := d.namedGORM("TestReclaimRetryIndexes").Raw("SELECT table_name,index_name,sql FROM duckdb_indexes() WHERE table_name='spans' ORDER BY index_name").Scan(&indexes).Error; err != nil {
			return err
		}
		plan := reclaimIndexPlan{Table: "spans", Staging: reclaimStagingTable("spans"), Indexes: indexes}
		if err := d.saveAndDropReclaimIndexes(plan); err != nil {
			return err
		}
		if err := d.recoverReclaimIndexes(); !errors.Is(err, forced) {
			return fmt.Errorf("restore failure not exercised: %w", err)
		}
		var before, after string
		if err := d.namedGORM("TestReclaimRetryPlanBefore").Raw("SELECT plan_json FROM retention_index_journal WHERE table_name=?", "spans").Scan(&before).Error; err != nil {
			return err
		}
		emptyPlan := reclaimIndexPlan{Table: "spans", Staging: plan.Staging}
		if err := d.saveAndDropReclaimIndexes(emptyPlan); err == nil {
			return errors.New("retry overwrote pending restoration plan")
		}
		if err := d.namedGORM("TestReclaimRetryPlanAfter").Raw("SELECT plan_json FROM retention_index_journal WHERE table_name=?", "spans").Scan(&after).Error; err != nil {
			return err
		}
		if before == "" || before != after {
			return errors.New("pending index definitions changed after failed retry")
		}
		failRestore = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The next public maintenance entry repairs the journal before doing work.
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	var plans int
	if err := d.SQL().QueryRow("SELECT count(*) FROM retention_index_journal WHERE active").Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if plans != 0 {
		t.Fatal("maintenance entry left an index restoration plan")
	}
	afterSchema, afterRows, afterIndexes := reclaimSnapshot(t, d)
	if schema != afterSchema || rows != afterRows || indexesBefore != afterIndexes {
		t.Fatal("retry lost schema, records or indexes")
	}
}

func TestReclaimIndexPlanRejectsUniqueBeforeMutation(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "unique-recovery", "unique-recovery", time.Now().UnixNano(), false, 128)
	if err := d.gorm.Exec("CREATE UNIQUE INDEX test_reclaim_unique_span_id ON spans(span_id)").Error; err != nil {
		t.Fatal(err)
	}
	schema, rows, indexes := reclaimSnapshot(t, d)
	if err := d.withMaintenance(func(d *DB) error {
		var selected []reclaimIndex
		if err := d.namedGORM("TestReclaimUniqueIndex").Raw("SELECT table_name,index_name,sql,is_unique FROM duckdb_indexes() WHERE index_name='test_reclaim_unique_span_id'").Scan(&selected).Error; err != nil {
			return err
		}
		if len(selected) != 1 || !selected[0].IsUnique {
			return errors.New("fixture is not a unique index")
		}
		plan := reclaimIndexPlan{Table: "spans", Staging: reclaimStagingTable("spans"), Indexes: selected}
		if err := d.saveAndDropReclaimIndexes(plan); err == nil {
			return errors.New("maintenance accepted a unique index")
		}
		var plans int64
		if err := d.namedGORM("TestReclaimUniquePlanCount").Raw("SELECT count(*) FROM retention_index_journal WHERE active").Scan(&plans).Error; err != nil {
			return err
		}
		if plans != 0 {
			return errors.New("unique-index rejection left a restoration plan")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	afterSchema, afterRows, afterIndexes := reclaimSnapshot(t, d)
	if schema != afterSchema || rows != afterRows || indexes != afterIndexes {
		t.Fatal("unique-index rejection changed schema, records or indexes")
	}
}

func TestReclaimIndexRestoresAfterCanceledWorkTransaction(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "canceled-reclaim", "canceled-reclaim", time.Now().UnixNano(), false, 128)
	beforeSchema, beforeRows, beforeIndexes := reclaimSnapshot(t, d)
	var indexes []reclaimIndex
	var defaults []reclaimDefault
	if err := d.gorm.Raw("SELECT table_name,index_name,sql,is_unique FROM duckdb_indexes() WHERE table_name='spans' ORDER BY index_name").Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	if err := d.gorm.Raw("SELECT column_name,column_default FROM duckdb_columns() WHERE table_name='spans' AND column_default IS NOT NULL").Scan(&defaults).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readerCtx, readerCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer readerCancel()
	readerHasLease := make(chan struct{})
	releaseReader := make(chan struct{})
	defer func() {
		select {
		case <-releaseReader:
		default:
			close(releaseReader)
		}
	}()
	type readResult struct {
		Count int
		Err   error
	}
	readerDone := make(chan readResult, 1)
	var workPool gorm.ConnPool
	injected, recoverySeen, checkpointSeen := false, false, false
	var fenceFailure error
	checkFence := func() {
		if _, err := workPool.ExecContext(context.Background(), "SELECT 1"); err != nil && fenceFailure == nil {
			fenceFailure = fmt.Errorf("maintenance lost its work lease: %w", err)
		}
		stats := d.SQL().Stats()
		if stats.InUse != stats.MaxOpenConnections && fenceFailure == nil {
			fenceFailure = fmt.Errorf("maintenance pool not drained: in_use=%d max=%d", stats.InUse, stats.MaxOpenConnections)
		}
		select {
		case <-readerHasLease:
			if fenceFailure == nil {
				fenceFailure = errors.New("queued reader acquired a connection before restoration/checkpoint finished")
			}
		default:
		}
	}
	const callback = "test_reclaim_canceled_transaction"
	d.gorm.Callback().Raw().After("gorm:raw").Register(callback, func(tx *gorm.DB) {
		sqlText := strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String()))
		if strings.HasPrefix(sqlText, "CREATE INDEX") {
			if !injected {
				injected = true
				waitBefore := d.SQL().Stats().WaitCount
				go func() {
					conn, err := d.SQL().Conn(readerCtx)
					if err != nil {
						readerDone <- readResult{Err: err}
						return
					}
					defer conn.Close()
					close(readerHasLease)
					select {
					case <-releaseReader:
					case <-readerCtx.Done():
						readerDone <- readResult{Err: readerCtx.Err()}
						return
					}
					var count int
					err = conn.QueryRowContext(readerCtx, "SELECT count(*) FROM spans").Scan(&count)
					readerDone <- readResult{Count: count, Err: err}
				}()
				until := time.Now().Add(time.Second)
				for d.SQL().Stats().WaitCount <= waitBefore && time.Now().Before(until) {
					time.Sleep(time.Millisecond)
				}
				if d.SQL().Stats().WaitCount <= waitBefore {
					fenceFailure = errors.New("reader did not enter the exhausted pool's wait queue")
				}
				// Cancel a real in-flight maintenance transaction after the first index
				// statement. Its SQL must remain bounded without surrendering its lease.
				cancel()
				tx.AddError(context.Canceled)
				return
			}
			recoverySeen = true
			checkFence()
		}
		if sqlText == "CHECKPOINT" && recoverySeen {
			checkpointSeen = true
			checkFence()
		}
	})
	defer d.gorm.Callback().Raw().Remove(callback)
	err = d.WithContext(ctx).withMaintenance(func(md *DB) error {
		workPool = md.gorm.Statement.ConnPool
		return md.reclaimUnconstrainedTable("spans", indexes, defaults)
	})
	close(releaseReader)
	if !injected || !recoverySeen || !checkpointSeen {
		t.Fatalf("did not exercise cancellation/restoration/checkpoint: injected=%v recovery=%v checkpoint=%v err=%v", injected, recoverySeen, checkpointSeen, err)
	}
	if fenceFailure != nil {
		t.Fatal(fenceFailure)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled optional work, got %v", err)
	}
	if errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("cleanup reused the discarded work connection: %v", err)
	}
	select {
	case result := <-readerDone:
		if result.Err != nil || result.Count != 128 {
			t.Fatalf("queued reader failed after recovery: %+v", result)
		}
	case <-readerCtx.Done():
		t.Fatal("queued reader did not resume after maintenance")
	}
	afterSchema, afterRows, afterIndexes := reclaimSnapshot(t, d)
	if beforeSchema != afterSchema || beforeRows != afterRows || beforeIndexes != afterIndexes {
		t.Fatal("canceled transaction lost records, defaults or indexes")
	}
	var pending int
	if err := d.SQL().QueryRow("SELECT count(*) FROM retention_index_journal WHERE active").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatal("canceled transaction left a pending index restoration")
	}
	if err := d.AppendSpan(&Span{TraceID: "after-canceled-reclaim", SpanID: "after-canceled-reclaim", Attributes: `{}`, Resource: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.SQL().QueryRow("SELECT count(*) FROM spans WHERE span_id='after-canceled-reclaim'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ingestion did not resume: rows=%d", count)
	}
}
