package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaintenanceDrainsSQLAndRestoresIngestion(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	held, err := d.SQL().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- d.withMaintenance(func(isolated *DB) error {
			close(entered)
			var value int
			if err := isolated.gorm.Raw("SELECT 1").Scan(&value).Error; err != nil {
				return err
			}
			if value != 1 {
				return errors.New("maintenance connection unusable")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			err := d.SQL().QueryRowContext(ctx, "SELECT 1").Scan(&value)
			if !errors.Is(err, context.DeadlineExceeded) {
				return errors.New("external SQL entered maintenance")
			}
			return nil
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for d.SQL().Stats().InUse < d.SQL().Stats().MaxOpenConnections {
		if time.Now().After(deadline) {
			t.Fatal("maintenance did not drain pool")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-entered:
		t.Fatal("maintenance overlapped active SQL")
	default:
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not release pool")
	}
	if err := d.AppendLog(&Log{Body: "after maintenance", Attributes: `{}`, ServiceName: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.SQL().QueryRow("SELECT count(*) FROM logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("logs=%d", count)
	}
}

func TestMaintenanceIgnoresCallerCancellationAfterPoolDrain(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(t.Context())
	scoped := d.WithContext(ctx)
	held, err := d.SQL().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	entered := make(chan struct{})
	proceed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- scoped.withMaintenance(func(isolated *DB) error {
			close(entered)
			<-proceed
			var value int
			return isolated.gorm.Raw("SELECT 1").Scan(&value).Error
		})
	}()

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not acquire the SQL pool")
	}
	cancel()
	close(proceed)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("maintenance inherited caller cancellation after pool drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not finish")
	}
}
