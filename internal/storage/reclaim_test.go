package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneSizeEvictsOnlyOldestTracesAboveCap(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "size.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.CreateSession("size", false)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8192)
	for i := range 5000 {
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		if err := d.AppendSpan(&Span{TraceID: fmt.Sprintf("trace-%08d", i), SpanID: fmt.Sprintf("%016x", i),
			SessionID: s.ID, Name: "sized operation", ServiceName: "size-test", StartNs: int64(i), EndNs: int64(i + 1),
			Attributes: `{"payload":"` + hex.EncodeToString(payload) + `"}`, Resource: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	cap := d.UsedSize() - 262144
	res, err := d.Prune(RetentionConfig{MaxDBSizeBytes: cap}, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize <= 0 || d.UsedSize() > cap {
		t.Fatalf("prune=%+v used=%d cap=%d", res, d.UsedSize(), cap)
	}
	var count, oldest, newest int64
	if err := d.SQL().QueryRow(`SELECT COUNT(*),MIN(start_ns),MAX(start_ns) FROM spans`).Scan(&count, &oldest, &newest); err != nil {
		t.Fatal(err)
	}
	if newest != 4999 || oldest != 5000-count || count < 4500 {
		t.Fatalf("eviction was not a small oldest prefix: count=%d oldest=%d newest=%d", count, oldest, newest)
	}
}

func TestPruneReclaimsIndexedDeletesBeforeEvictingLiveTraces(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "retention.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.CreateSession("retention", false)
	if err != nil {
		t.Fatal(err)
	}
	// Fresh-schema setup currently omits the historical performance indexes.
	// Install them explicitly so this regression exercises indexed row groups.
	if err := d.withMaintenance(func(isolated *DB) error {
		return execMigrationFile(isolated.gorm, "0007_performance_indexes.sql")
	}); err != nil {
		t.Fatal(err)
	}
	for i := range 20000 {
		if err := d.AppendSpan(&Span{TraceID: fmt.Sprintf("trace-%08d", i), SpanID: fmt.Sprintf("%016x", i),
			SessionID: s.ID, Name: fmt.Sprintf("operation-%08d", i), ServiceName: "retention-test",
			StartNs: int64(i), EndNs: int64(i + 1), ReceivedAt: int64(i), Attributes: `{}`, Resource: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := d.withMaintenance(func(isolated *DB) error {
		if err := isolated.gorm.Exec(`DELETE FROM spans WHERE start_ns < 19900`).Error; err != nil {
			return err
		}
		return isolated.checkpointWithRetry()
	}); err != nil {
		t.Fatal(err)
	}
	before := d.UsedSize()
	var indexesBefore int
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM duckdb_indexes() WHERE table_name='spans'`).Scan(&indexesBefore); err != nil {
		t.Fatal(err)
	}
	t.Logf("before reclaim: bytes=%d span indexes=%d", before, indexesBefore)
	if indexesBefore == 0 {
		t.Fatal("fixture must contain indexed span rows")
	}
	// The old implementation deleted live traces immediately. Reclamation must
	// satisfy this cap without evicting any of the 100 surviving trace IDs.
	res, err := d.Prune(RetentionConfig{MaxDBSizeBytes: before - 262144}, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize != 0 {
		t.Fatalf("deleted %d live spans for stale allocation", res.DeletedBySize)
	}
	if after := d.UsedSize(); after >= before {
		t.Fatalf("indexed deletes were not reclaimed: before=%d after=%d", before, after)
	}
	var count int64
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM spans`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Fatalf("live spans=%d, want 100", count)
	}
	// Rebound appenders must address the replacement table, and constraints and
	// secondary indexes must still enforce/query the same schema.
	if err := d.AppendSpan(&Span{TraceID: "after", SpanID: "after", SessionID: s.ID, Name: "after", Attributes: `{}`, Resource: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM spans WHERE trace_id = 'after'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("appender wrote to old table: count=%d", count)
	}
	var indexes int
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM duckdb_indexes() WHERE table_name='spans'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != indexesBefore {
		t.Fatalf("rebuild changed span indexes: before=%d after=%d", indexesBefore, indexes)
	}
}

func TestRetentionBatchUsesGlobalAgeAndPreservesNewerSignals(t *testing.T) {
	d := openTestDB(t)
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 100)
	if err := d.InsertLog(&Log{Body: "oldest", TimestampNs: 1, SessionID: "active", Attributes: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertLog(&Log{Body: "newest", TimestampNs: time.Now().Add(time.Hour).UnixNano(), SessionID: "active", Attributes: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.withMaintenance(func(isolated *DB) error {
		_, err := isolated.deleteOldestTelemetryBatch("active")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var old, recent, spans int
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FILTER(WHERE body='oldest'), COUNT(*) FILTER(WHERE body='newest') FROM logs`).Scan(&old, &recent); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM spans`).Scan(&spans); err != nil {
		t.Fatal(err)
	}
	if old != 0 || recent != 1 || spans != 99 {
		t.Fatalf("old=%d recent=%d spans=%d", old, recent, spans)
	}
}

func TestPruneUnreachableCapDoesNotRepeatedlyEraseTelemetry(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "floor.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 100)
	for range 5 {
		_, _ = d.Prune(RetentionConfig{MaxDBSizeBytes: 1}, "active")
	}
	var count int
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM spans`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 95 {
		t.Fatalf("unreachable cap erased live traces: remaining=%d", count)
	}
}
