package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledAppenderOperationsLeaveNativeQueue(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for name, operation := range map[string]func(*DB) error{
		"span":   func(d *DB) error { return d.AppendSpan(&Span{TraceID: "canceled", SpanID: "canceled"}) },
		"log":    func(d *DB) error { return d.AppendLog(&Log{Body: "canceled"}) },
		"metric": func(d *DB) error { return d.AppendMetric(&Metric{Name: "canceled"}) },
		"flush":  func(d *DB) error { return d.FlushBatch() },
	} {
		t.Run(name, func(t *testing.T) {
			// Simulate a native flush or maintenance holding the gate. An export
			// whose client leaves must release admission without waiting for it.
			d.batcher.mu.Lock()
			defer d.batcher.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				close(started)
				done <- operation(d.WithContext(ctx))
			}()
			<-started
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled export returned %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("canceled export remained queued behind native work")
			}
		})
	}
	if err := d.AppendLog(&Log{Body: "healthy", Attributes: "{}", ServiceName: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var spans, logs, metrics int
	if err := d.SQL().QueryRow("SELECT (SELECT count(*) FROM spans), (SELECT count(*) FROM logs), (SELECT count(*) FROM metrics)").Scan(&spans, &logs, &metrics); err != nil {
		t.Fatal(err)
	}
	if spans != 0 || logs != 1 || metrics != 0 {
		t.Fatalf("canceled writes persisted or fresh ingestion broke: spans=%d logs=%d metrics=%d", spans, logs, metrics)
	}
}

func TestMaintenanceWaitsForCompleteExport(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	finishExport, err := d.BeginIngest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	exportOpen := true
	defer func() {
		if exportOpen {
			finishExport()
		}
	}()
	entered := make(chan struct{})
	finishMaintenance := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- d.withMaintenance(func(*DB) error {
			close(entered)
			<-finishMaintenance
			return nil
		})
	}()
	maintenanceOpen := true
	defer func() {
		if maintenanceOpen {
			close(finishMaintenance)
		}
	}()
	select {
	case <-entered:
		t.Fatal("maintenance interrupted an admitted export")
	case <-time.After(100 * time.Millisecond):
	}
	// The admitted request can finish all its rows and flush even with a
	// maintenance writer queued; pausing halfway through caused live timeouts.
	writeCtx, cancelWrite := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelWrite()
	scoped := d.WithContext(writeCtx)
	if err := scoped.AppendLog(&Log{Body: "complete export", Attributes: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	finishExport()
	exportOpen = false
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not acquire the drained export gate")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if release, err := d.BeginIngest(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("new export bypassed maintenance: %v", err)
	}
	// Let maintenance complete before closing the DB.
	close(finishMaintenance)
	maintenanceOpen = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance failed to release after the complete export")
	}
}
