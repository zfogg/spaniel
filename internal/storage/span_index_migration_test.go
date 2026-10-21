package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionIndexMigrationPreservesRowsLookupAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaniel.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "index", "index", time.Now().UnixNano(), false, 10000)
	if err := d.withMaintenance(func(d *DB) error { return execMigrationFile(d.gorm, "0007_performance_indexes.sql") }); err != nil {
		t.Fatal(err)
	}
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	before := d.UsedSize()
	var beforeKey, beforePlan string
	if err := d.SQL().QueryRow("EXPLAIN SELECT * FROM spans WHERE trace_id='index-t0'").Scan(&beforeKey, &beforePlan); err != nil {
		t.Fatal(err)
	}
	var payload string
	query := "SELECT CAST(to_json(list(row(trace_id,span_id,parent_span_id,name,start_ns,end_ns,attributes,resource,session_id) ORDER BY span_id)) AS VARCHAR) FROM spans"
	if err := d.SQL().QueryRow(query).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := d.gorm.Exec("DELETE FROM migrations WHERE id='0040_remove_unused_span_indexes'").Error; err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	var afterPayload string
	if err := d.SQL().QueryRow(query).Scan(&afterPayload); err != nil {
		t.Fatal(err)
	}
	if payload != afterPayload {
		t.Fatal("index migration changed telemetry")
	}
	var indexes int
	if err := d.SQL().QueryRow("SELECT count(*) FROM duckdb_indexes() WHERE table_name='spans'").Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 4 {
		t.Fatalf("lookup indexes=%d, want4", indexes)
	}
	if after := d.UsedSize(); after >= before {
		t.Fatalf("unused indexes not reclaimed: before=%d after=%d", before, after)
	}
	var key, plan string
	if err := d.SQL().QueryRow("EXPLAIN SELECT * FROM spans WHERE trace_id='index-t0'").Scan(&key, &plan); err != nil {
		t.Fatal(err)
	}
	if beforePlan != plan {
		t.Fatalf("trace lookup plan changed: before=%s after=%s", beforePlan, plan)
	}
	spans, err := d.GetTrace("index-t0")
	if err != nil || len(spans) != 1 {
		t.Fatalf("lookup result spans=%d err=%v", len(spans), err)
	}
}
