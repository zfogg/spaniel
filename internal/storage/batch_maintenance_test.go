package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestBatcherMaintenanceRestoresPinnedLeasesWithReadersWaiting(t *testing.T) {
	for _, outcome := range []string{"success", "error", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			d, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := d.AppendLog(&Log{Body: "before maintenance", Attributes: `{}`}); err != nil {
				t.Fatal(err)
			}
			b := d.batcher
			leases := make(map[string]*sql.Conn)
			b.mu.Lock()
			for table, appender := range b.apps {
				leases[table] = appender.conn
			}
			b.mu.Unlock()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			callbackErr := errors.New("maintenance callback failed")
			var readers []*sql.Conn
			ready, done := make(chan struct{}), make(chan error, 1)
			go func() {
				done <- b.Maintenance(ctx, func(pinned int) error {
					defer close(ready)
					if pinned != len(batchTables) {
						return errors.New("maintenance did not retain all appender leases")
					}
					for table, appender := range b.apps {
						if appender.app != nil || appender.conn != leases[table] {
							return errors.New("native appender alive or connection lease changed during maintenance")
						}
					}
					// Simulate readers occupying every general SQL slot as maintenance
					// ends. Restoration must not need any of those slots to finish.
					for i := 0; i < d.SQL().Stats().MaxOpenConnections-pinned; i++ {
						conn, err := d.SQL().Conn(ctx)
						if err != nil {
							return err
						}
						readers = append(readers, conn)
						if i == 0 {
							if _, err := conn.ExecContext(ctx, "CHECKPOINT"); err != nil {
								return err
							}
						}
					}
					switch outcome {
					case "error":
						return callbackErr
					case "cancel":
						cancel()
						return ctx.Err()
					default:
						return nil
					}
				})
			}()
			select {
			case <-ready:
			case <-time.After(15 * time.Second):
				t.Fatal("maintenance did not reach restoration")
			}
			releaseReaders := func() {
				for _, conn := range readers {
					_ = conn.Close()
				}
				readers = nil
			}
			defer releaseReaders()
			select {
			case err := <-done:
				switch outcome {
				case "error":
					if !errors.Is(err, callbackErr) {
						t.Fatalf("callback error lost: %v", err)
					}
				case "cancel":
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation lost: %v", err)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
				}
			case <-time.After(5 * time.Second):
				releaseReaders()
				<-done
				t.Fatal("appender restoration waited for readers to release SQL slots")
			}
			b.mu.Lock()
			for table, appender := range b.apps {
				if appender.app == nil || appender.conn != leases[table] {
					t.Errorf("appender %s not restored on its original lease", table)
				}
			}
			b.mu.Unlock()
			// Ingestion must work even while all general reader slots remain held.
			if err := d.AppendLog(&Log{Body: "after maintenance", Attributes: `{}`}); err != nil {
				t.Fatal(err)
			}
			if err := d.FlushBatch(); err != nil {
				t.Fatal(err)
			}
			releaseReaders()
			var count int
			if err := d.SQL().QueryRow("SELECT count(*) FROM logs").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatalf("maintenance lost buffered rows: %d", count)
			}
		})
	}
}

func TestBatcherMaintenanceCanceledWhileWaitingLeavesAppendersOpen(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b := d.batcher
	b.mu.Lock()
	locked := true
	defer func() {
		if locked {
			b.mu.Unlock()
		}
	}()
	app := b.apps["logs"].app
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	called := false
	done := make(chan error, 1)
	go func() { done <- b.Maintenance(ctx, func(int) error { called = true; return nil }) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting maintenance ignored deadline: %v", err)
		}
	case <-time.After(2 * time.Second):
		b.mu.Unlock()
		locked = false
		<-done
		t.Fatal("maintenance waited beyond its work context")
	}
	if called || b.apps["logs"].app != app {
		t.Fatal("expired maintenance started a second ingestion pause")
	}
	b.mu.Unlock()
	locked = false
	if err := d.AppendLog(&Log{Body: "after canceled wait", Attributes: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
}

func TestBatcherMaintenanceClosedHasNoPinnedLeases(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.batcher.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.batcher.Maintenance(t.Context(), func(pinned int) error {
		if pinned != 0 {
			return errors.New("closed batcher reported reserved connections")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBatcherMaintenanceRestoresOtherTablesAfterReopenFailure(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err = d.batcher.Maintenance(ctx, func(int) error {
		if err := d.gorm.Exec("DROP TABLE logs").Error; err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || onlyError(err, context.Canceled) {
		t.Fatalf("reopen failure was lost or classified as pure cancellation: %v", err)
	}
	d.batcher.mu.Lock()
	for _, table := range batchTables {
		if table != "logs" && d.batcher.apps[table].app == nil {
			t.Errorf("healthy %s appender left closed after another table failed", table)
		}
	}
	d.batcher.mu.Unlock()
	if err := d.AppendMetric(&Metric{Name: "after_failed_reopen", Type: "gauge", Attributes: `{}`, Exemplars: `[]`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.SQL().QueryRow("SELECT count(*) FROM metrics").Scan(&count); err != nil || count != 1 {
		t.Fatalf("healthy appender could not persist after failed restoration: count=%d err=%v", count, err)
	}
}

func TestBatcherMaintenanceDuringCloseReleasesLeasesBeforeCallback(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b := d.batcher
	// Pause Close at its flusher join, after it publishes closed but before
	// it can release native appenders. The real flusher is already joined.
	close(b.stop)
	<-b.done
	b.stop = make(chan struct{})
	join := make(chan struct{})
	b.done = join
	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	defer func() {
		close(join)
		if err := <-closed; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		closing := b.closed
		b.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not reach its flusher join")
		}
		time.Sleep(time.Millisecond)
	}
	if err := b.Maintenance(t.Context(), func(pinned int) error {
		if pinned != 0 || d.SQL().Stats().InUse != 0 {
			return errors.New("closing batcher exposed live appender leases to maintenance")
		}
		_, err := d.SQL().ExecContext(t.Context(), "CHECKPOINT")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
