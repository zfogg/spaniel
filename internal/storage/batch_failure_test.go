package storage

import (
	"strings"
	"testing"
)

// A failed append can invalidate earlier buffered rows when the appender is
// recreated. Even a successful background flush must not hide that failure
// from the export acknowledgment barrier.
func TestBatcherFailureSurvivesBackgroundFlush(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	version := db.BatchFailureVersion()
	if err := db.AppendLog(&Log{Body: "before failure", Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := db.batcher.append("logs", "wrong column count"); err == nil {
		t.Fatal("invalid append succeeded")
	}
	if err := db.batcher.flush(false); err == nil {
		t.Fatal("background flush forgot append failure")
	}
	if err := db.FlushBatch(); err == nil || !strings.Contains(err.Error(), "append logs") {
		t.Fatalf("acknowledgment lost failure: %v", err)
	}
	if err := db.FlushBatchSince(version); err == nil {
		t.Fatal("another explicit flush consumed an executing group failure")
	}
	version = db.BatchFailureVersion()
	if err := db.AppendLog(&Log{Body: "after failure", Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatalf("acknowledged failure poisoned later exports: %v", err)
	}
}

func TestBatcherMaintenanceCloseFailureReachesAcknowledgment(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Stop only the interval worker while constructing a native close-time
	// constraint failure. Restart it before Close so normal ownership is kept.
	b := db.batcher
	close(b.stop)
	<-b.done
	b.stop, b.done = make(chan struct{}), make(chan struct{})
	defer func() { go b.loop() }()
	if err := db.withMaintenance(func(d *DB) error {
		return d.gorm.Exec("CREATE UNIQUE INDEX test_log_body_unique ON logs(body)").Error
	}); err != nil {
		t.Fatal(err)
	}
	version := db.BatchFailureVersion()
	for range 2 {
		if err := db.AppendLog(&Log{Body: "duplicate buffered value", Attributes: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.withMaintenance(func(*DB) error { return nil }); err == nil || !strings.Contains(err.Error(), "close appender for logs") {
		t.Fatalf("native close did not expose its constraint failure: %v", err)
	}
	if err := db.FlushBatch(); err == nil {
		t.Fatal("maintenance failure was lost at the acknowledgment barrier")
	}
	if err := db.FlushBatchSince(version); err == nil {
		t.Fatal("another flush hid the maintenance failure from an executing group")
	}
	version = db.BatchFailureVersion()
	if err := db.AppendLog(&Log{Body: "healthy after close failure", Attributes: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := db.FlushBatchSince(version); err != nil {
		t.Fatalf("restored appender did not recover: %v", err)
	}
}

func TestBatcherMaintenanceReopenFailureReachesAcknowledgment(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	version := db.BatchFailureVersion()
	// Renaming the table produces a real native appender restoration failure.
	if err := db.withMaintenance(func(d *DB) error {
		return d.gorm.Exec("ALTER TABLE logs RENAME TO test_logs_paused").Error
	}); err == nil || !strings.Contains(err.Error(), "reopen appender for logs") {
		t.Fatalf("missing native restoration error: %v", err)
	}
	if err := db.FlushBatchSince(version); err == nil {
		t.Fatal("missing appender was acknowledged successfully")
	}
	if err := db.withMaintenance(func(d *DB) error {
		return d.gorm.Exec("ALTER TABLE test_logs_paused RENAME TO logs").Error
	}); err != nil {
		t.Fatal(err)
	}
	version = db.BatchFailureVersion()
	if err := db.AppendLog(&Log{Body: "healthy after restoration failure", Attributes: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := db.FlushBatchSince(version); err != nil {
		t.Fatalf("restored table did not recover: %v", err)
	}
}
