package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionSoftFailureDoesNotHideJoinedStorageErrors(t *testing.T) {
	soft := fmt.Errorf("target: %w", ErrRetentionTargetUnreached)
	if !IsRetentionTargetUnreached(errors.Join(soft, soft)) {
		t.Fatal("pure target failure should be classified")
	}
	for _, err := range []error{nil, errors.New("I/O failed"), errors.Join(soft, errors.New("appender reopen failed"))} {
		if IsRetentionTargetUnreached(err) {
			t.Fatalf("genuine failure classified soft: %v", err)
		}
	}
	if !IsMaintenanceTimeout(fmt.Errorf("drain: %w", context.DeadlineExceeded)) || IsMaintenanceTimeout(errors.Join(context.DeadlineExceeded, errors.New("reopen failed"))) {
		t.Fatal("maintenance deadline classification hid a restoration failure")
	}
}

func TestRetentionBudgetBackoffPrecedesMaintenanceAndPreservesWrites(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "budget.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 100)
	cfg := RetentionConfig{MaxDBSizeBytes: 1, MaxDuration: time.Nanosecond}
	_, err = d.Prune(cfg, "active")
	if !IsRetentionTargetUnreached(err) {
		t.Fatalf("expired budget must be soft: %v", err)
	}
	if d.retentionRetry.Load() <= time.Now().UnixNano() {
		t.Fatal("missing retry backoff")
	}
	// Draining the pool would block behind this connection. A retry must return
	// from the shared policy cache before acquiring maintenance or querying sizes.
	held, err := d.SQL().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	start := time.Now()
	_, err = d.Prune(cfg, "active")
	if !IsRetentionTargetUnreached(err) || time.Since(start) > time.Second {
		t.Fatalf("backoff entered maintenance: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendLog(&Log{Body: "after budget", Attributes: `{}`, SessionID: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.SQL().QueryRow("SELECT count(*) FROM spans").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Fatalf("budget erased spans: %d", count)
	}
}

func TestStorageBreakdownDoesNotDrainOtherReaders(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "read.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	held, err := d.SQL().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := d.WithContext(ctx).GetStorageBreakdown(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("read-only storage view entered exclusive maintenance: %v", ctx.Err())
	}
}

func TestRetentionAutomaticNoWorkDoesNotDrainReaders(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "idle.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 2)
	held, err := d.SQL().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, err = d.Prune(RetentionConfig{
		MaxAge: 24 * time.Hour, MaxSessions: 10,
		MaxDBSizeBytes: 500 << 20, MaxDuration: time.Second,
	}, "active")
	if err != nil {
		t.Fatalf("no-work automatic check entered exclusive maintenance: %v", err)
	}
	if err := d.AppendLog(&Log{Body: "no maintenance pause", Attributes: `{}`, SessionID: "active"}); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionAutomaticCheckpointSavingsDoNotStarveOldestEviction(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 1000)
	for i := range 2 {
		if err := d.AppendSpan(&Span{TraceID: fmt.Sprintf("new%d", i), SpanID: fmt.Sprintf("new%d", i), SessionID: "active", StartNs: time.Now().Add(time.Hour).UnixNano(), Attributes: `{"padding":"` + strings.Repeat("padding", 250000) + `"}`, Resource: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	res, err := d.Prune(RetentionConfig{MaxDBSizeBytes: 1, MaxDuration: 6 * time.Second}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize != 0 || !d.retentionCheckpointReady.Load() {
		t.Fatal("first automatic pass must checkpoint without deleting telemetry")
	}
	res, err = d.Prune(RetentionConfig{MaxDBSizeBytes: 1, MaxDuration: 6 * time.Second}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize <= 0 {
		t.Fatal("checkpoint/WAL savings indefinitely postponed eviction")
	}
	var recent int
	if err := d.SQL().QueryRow("SELECT count(*) FROM spans WHERE span_id IN ('new0','new1')").Scan(&recent); err != nil {
		t.Fatal(err)
	}
	if recent != 2 {
		t.Fatal("bounded pass deleted newest records")
	}
}

func TestRetentionAutomaticRebuildCheckpointsOnlyOneDirtyTable(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 1000)
	for range 1000 {
		if err := d.AppendLog(&Log{Body: "one table phase", Attributes: `{}`, SessionID: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	if err := d.withMaintenance(func(d *DB) error {
		if err := d.gorm.Exec("DELETE FROM logs WHERE rowid % 2 = 0").Error; err != nil {
			return err
		}
		if err := d.gorm.Exec("DELETE FROM spans WHERE span_id LIKE '%0'").Error; err != nil {
			return err
		}
		return d.checkpointWithRetry()
	}); err != nil {
		t.Fatal(err)
	}
	dirty := func(table string) bool {
		t.Helper()
		var stored, live int64
		if err := d.SQL().QueryRow("SELECT estimated_size FROM duckdb_tables() WHERE table_name=?", table).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if err := d.SQL().QueryRow("SELECT count(*) FROM " + table).Scan(&live); err != nil {
			t.Fatal(err)
		}
		return stored > live
	}
	if !dirty("logs") || !dirty("spans") {
		t.Fatal("fixture has no deleted tuples")
	}
	// Simulate a prior oldest-prefix eviction. A first small-table rebuild
	// cannot declare the cycle unreclaimable while spans are still dirty.
	d.retentionFloor.Store(d.UsedSize())
	d.retentionCheckpointReady.Store(true)
	if _, err := d.Prune(RetentionConfig{MaxDBSizeBytes: 1, MaxDuration: 6 * time.Second}, "active"); err != nil {
		t.Fatal(err)
	}
	if d.RetentionBackingOff() || !d.retentionCheckpointReady.Load() || dirty("logs") || !dirty("spans") {
		t.Fatal("first table declared a storage floor before remaining tables were reclaimed")
	}
	// A fresh deletion in an already visited small table must not requeue it
	// ahead of the large table still pending in this same reclamation cycle.
	if err := d.gorm.Exec("DELETE FROM logs WHERE rowid % 3 = 0").Error; err != nil {
		t.Fatal(err)
	}
	progress := reclaimProgress{OneTable: true, Visited: d.retentionRebuildVisited.Load()}
	if err := d.withMaintenance(func(d *DB) error { return d.rebuildTelemetry(&progress) }); err != nil {
		t.Fatal(err)
	}
	if !progress.Rebuilt || progress.Remaining || dirty("spans") || !dirty("logs") {
		t.Fatal("second table did not complete reclamation")
	}
}

func TestRetentionAutomaticCheckpointReadinessExpiresWithLargeNewWAL(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "active", "active", time.Now().UnixNano(), false, 10)
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	d.retentionCheckpointReady.Store(true)
	for i := range 12 {
		if err := d.AppendLog(&Log{Body: fmt.Sprint(i) + strings.Repeat("x", 1<<20), Attributes: `{}`, SessionID: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	if d.WALSize() < ManagedWALCheckpointBytes {
		t.Fatal("fixture did not accumulate new WAL")
	}
	res, err := d.Prune(RetentionConfig{MaxDBSizeBytes: 1, MaxDuration: 6 * time.Second}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize != 0 || !d.retentionCheckpointReady.Load() {
		t.Fatal("stale checkpoint readiness allowed eviction in a large-WAL pause")
	}
	var count int
	if err := d.SQL().QueryRow("SELECT count(*) FROM logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("checkpoint phase deleted logs: %d", count)
	}
}
