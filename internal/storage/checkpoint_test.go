package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Exercise real indexed tables and a real WAL: in-memory databases never
// take DuckDB's automatic checkpoint path and cannot catch this regression.
func TestCheckpointOwnsWALLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})
	var version string
	if err := db.SQL().QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("DuckDB %s", version)
	// Make every write eligible for an engine checkpoint. It must still stay
	// in the WAL until Spaniel closes all appenders at its maintenance gate.
	if _, err := db.SQL().Exec("SET checkpoint_threshold = '1KB'"); err != nil {
		t.Fatal(err)
	}
	for cycle := range 4 {
		for i := range 32 {
			id := fmt.Sprintf("%d-%d", cycle, i)
			if err := db.AppendSpan(&Span{TraceID: id, SpanID: id, Name: "checkpoint", StartNs: 1, EndNs: 2, Attributes: "{}", Resource: "{}"}); err != nil {
				t.Fatal(err)
			}
			if err := db.AppendLog(&Log{Body: id, TimestampNs: 1, Attributes: "{}"}); err != nil {
				t.Fatal(err)
			}
			if err := db.AppendMetric(&Metric{Name: id, Type: "gauge", TimestampNs: 1, Value: 1, Attributes: "{}"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.FlushBatch(); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path + ".wal")
		if err != nil || info.Size() < 1024 {
			t.Fatalf("cycle %d: write checkpointed outside maintenance: WAL=%v err=%v", cycle, info, err)
		}
		if err := db.batcher.checkpointIfNeeded(1024); err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(path + ".wal")
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err == nil && info.Size() >= 1024 {
			t.Fatalf("cycle %d: guarded checkpoint left %d WAL bytes", cycle, info.Size())
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range batchTables {
		var count int
		if err := db.SQL().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 128 {
			t.Fatalf("%s: reopened rows = %d, want 128", table, count)
		}
	}
}

func TestCheckpointConcurrentIngestionAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})
	seedSession(t, db, "expired", "expired", time.Now().Add(-48*time.Hour).UnixNano(), false, 10)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for writer := range 3 {
		wg.Go(func() {
			for i := range 500 {
				id := fmt.Sprintf("%d-%d", writer, i)
				if err := db.AppendSpan(&Span{TraceID: id, SpanID: id, Name: "concurrent", StartNs: 1, EndNs: 2, Attributes: "{}", Resource: "{}"}); err != nil {
					errs <- err
					return
				}
				if i%25 == 0 {
					if err := db.FlushBatch(); err != nil {
						errs <- err
						return
					}
				}
			}
		})
	}
	wg.Go(func() {
		for range 8 {
			if _, err := db.Prune(RetentionConfig{MaxAge: 24 * time.Hour}, ""); err != nil {
				errs <- err
				return
			}
			if err := db.batcher.checkpointIfNeeded(1); err != nil {
				errs <- err
				return
			}
		}
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQL().QueryRow("SELECT count(*) FROM spans").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1500 {
		t.Fatalf("reopened spans = %d, want 1500", count)
	}
}
