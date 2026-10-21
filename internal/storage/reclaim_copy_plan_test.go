package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestReclaimCopyUsesSerialPlanAndBoundedWAL(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	const rows = 4*122880 + 777
	const fields = "trace_id,span_id,parent_span_id,service_name,name,kind,start_ns,end_ns,duration_ns,status_code,status_message,attributes,resource,session_id,session_label,received_at,sampled"
	type fingerprint struct {
		Rows int64
		Hash string
	}
	if err := d.withMaintenance(func(d *DB) error {
		// Four full row groups give each native worker a final batch. DuckDB1.1's
		// ordered batch insert can misclassify those groups as already persisted,
		// turning their repeated resource payload into a gigabyte of logical WAL.
		seed := fmt.Sprintf(`CREATE TABLE copy_plan_source AS SELECT
   md5(i::VARCHAR) AS trace_id, substr(md5(i::VARCHAR),1,16) AS span_id,
   CASE WHEN i%%2=0 THEN '' ELSE 'parent' END AS parent_span_id,
   'copy-plan-test' AS service_name, 'operation-'||i AS name,
   (i%%6)::INTEGER AS kind, (i*1000)::BIGINT AS start_ns,
   (i*1000+10)::BIGINT AS end_ns, 10::BIGINT AS duration_ns,
   (i%%3)::INTEGER AS status_code, 'status-'||i%%3 AS status_message,
   md5(i::VARCHAR) AS attributes, repeat('r',2048) AS resource,
   'copy-plan-session' AS session_id, 'copy plan' AS session_label,
   i::BIGINT AS received_at, i%%2=0 AS sampled
   FROM range(%d)t(i)`, rows)
		if err := d.namedGORM("TestReclaimCopySource").Exec(seed).Error; err != nil {
			return err
		}
		if err := d.checkpointWithRetry(); err != nil {
			return err
		}
		var threads int
		if err := d.namedGORM("TestReclaimCopyThreads").Raw("SELECT current_setting('threads')").Scan(&threads).Error; err != nil {
			return err
		}
		if threads != 4 {
			return fmt.Errorf("regression requires four DuckDB workers, got %d", threads)
		}
		sql := reclaimCopySQL("copy_plan_source", "copy_plan_target")
		var plan []struct{ ExplainKey, ExplainValue string }
		if err := d.namedGORM("TestReclaimCopyPlan").Raw("EXPLAIN " + sql).Scan(&plan).Error; err != nil {
			return err
		}
		var physical strings.Builder
		for _, p := range plan {
			physical.WriteString(p.ExplainValue)
		}
		if strings.Contains(physical.String(), "BATCH_CREATE_TABLE_AS") || !strings.Contains(physical.String(), "CREATE_TABLE_AS") {
			return fmt.Errorf("reclamation selected unsafe batch copy: %s", physical.String())
		}
		var before fingerprint
		hashSQL := "SELECT count(*) AS rows,CAST(sum(CAST(hash(" + fields + ") AS HUGEINT)) AS VARCHAR) AS hash FROM "
		if err := d.namedGORM("TestReclaimCopyBefore").Raw(hashSQL + "copy_plan_source").Scan(&before).Error; err != nil {
			return err
		}
		if err := d.namedGORM("TestReclaimCopyExecute").Exec(sql).Error; err != nil {
			return err
		}
		wal := d.WALSize()
		if wal >= 64*1024*1024 {
			return fmt.Errorf("copy serialized raw resource payload into WAL: %d bytes", wal)
		}
		var after fingerprint
		if err := d.namedGORM("TestReclaimCopyAfter").Raw(hashSQL + "copy_plan_target").Scan(&after).Error; err != nil {
			return err
		}
		if before.Rows != rows || before != after {
			return fmt.Errorf("copy changed all-field fingerprint: before=%+v after=%+v", before, after)
		}
		t.Logf("serial copy rows=%d WAL=%d all17fieldhash=%s", after.Rows, wal, after.Hash)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
