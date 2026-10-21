package ingestion

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func awaitBatch(t *testing.T, b *ingestBatcher, w *batchWork) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- b.wait(w) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not finish")
		return nil
	}
}

func TestSharedBatchCoalescesAndPublishesAfterFlush(t *testing.T) {
	var flushes, processed, published, notified atomic.Int64
	flushed := false
	b := newIngestBatcher(func(context.Context, bool) error {
		if processed.Load() != 100 {
			t.Errorf("flushed before all requests: %d", processed.Load())
		}
		if published.Load() != 0 {
			t.Error("published before flush")
		}
		flushed = true
		flushes.Add(1)
		return nil
	}, func() { notified.Add(1) })
	b.window = time.Hour
	var work []*batchWork
	for range 100 {
		w, err := b.enqueue(t.Context(), 1, 100, func(ctx context.Context) error {
			processed.Add(1)
			stage := effects(ctx)
			stage.live = true
			stage.after = append(stage.after, func() {
				if !flushed {
					t.Error("notification before flush")
				}
				published.Add(1)
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		work = append(work, w)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, w := range work {
		if err := awaitBatch(t, b, w); err != nil {
			t.Fatal(err)
		}
	}
	if flushes.Load() != 1 || published.Load() != 100 || notified.Load() != 1 {
		t.Fatalf("flushes=%d published=%d notifications=%d", flushes.Load(), published.Load(), notified.Load())
	}
	if b.requests != 0 || b.bytes != 0 {
		t.Fatal("capacity leaked")
	}
}

func TestSharedBatchTriggers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rows, bytes int
	}{
		{"rows", batchRows, 1}, {"bytes", 1, batchBytes}, {"oversized", 1, queueBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newIngestBatcher(func(context.Context, bool) error { return nil }, func() {})
			b.window = time.Hour
			w, err := b.enqueue(t.Context(), tc.rows, tc.bytes, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if err = awaitBatch(t, b, w); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSharedBatchOldestDeadlineDoesNotDebounce(t *testing.T) {
	flushed := make(chan struct{}, 100)
	b := newIngestBatcher(func(context.Context, bool) error { flushed <- struct{}{}; return nil }, func() {})
	b.window = 80 * time.Millisecond
	start := time.Now()
	first, err := b.enqueue(t.Context(), 1, 1, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _ = b.enqueue(context.Background(), 1, 1, func(context.Context) error { return nil })
			}
		}
	}()
	if err := awaitBatch(t, b, first); err != nil {
		t.Fatal(err)
	}
	close(stop)
	<-producerDone
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("continuous traffic postponed oldest export")
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSharedBatchCancellationAndOverload(t *testing.T) {
	b := newIngestBatcher(func(context.Context, bool) error { return nil }, func() {})
	b.window = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	w, err := b.enqueue(ctx, 1, 100, func(context.Context) error { t.Error("canceled work executed"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := awaitBatch(t, b, w); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if b.requests != 0 || b.bytes != 0 {
		t.Fatal("canceled queue capacity retained")
	}
	// Hold the executing batch so overload accounts for its bytes too.
	entered, release := make(chan struct{}), make(chan struct{})
	w, err = b.enqueue(t.Context(), batchRows, queueBytes, func(context.Context) error { close(entered); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err = b.enqueue(t.Context(), 1, 1, func(context.Context) error { return nil }); !errors.Is(err, ErrIngestBusy) {
		t.Fatalf("overload: %v", err)
	}
	close(release)
	if err := awaitBatch(t, b, w); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = b.enqueue(t.Context(), 1, 1, nil); !errors.Is(err, ErrIngestClosed) {
		t.Fatalf("closed: %v", err)
	}
}

func TestSharedBatchRequestLimitAndShutdownDrain(t *testing.T) {
	b := newIngestBatcher(func(context.Context, bool) error { return nil }, func() {})
	b.window = time.Hour
	entered, release := make(chan struct{}), make(chan struct{})
	w, err := b.enqueue(t.Context(), batchRows, 1, func(context.Context) error { close(entered); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	for range queueRequests - 1 {
		if _, err := b.enqueue(t.Context(), 0, 0, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.enqueue(t.Context(), 0, 0, nil); !errors.Is(err, ErrIngestBusy) {
		t.Fatalf("request limit: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := b.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain timeout: %v", err)
	}
	close(release)
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := awaitBatch(t, b, w); err != nil {
		t.Fatal(err)
	}
	if b.requests != 0 || b.bytes != 0 {
		t.Fatal("drain leaked capacity")
	}
}

func TestSharedBatchFailuresReachEveryExporter(t *testing.T) {
	for _, appendFailure := range []bool{false, true} {
		t.Run(map[bool]string{true: "append", false: "flush"}[appendFailure], func(t *testing.T) {
			failure := errors.New("disk full during shared write")
			b := newIngestBatcher(func(context.Context, bool) error {
				if !appendFailure {
					return failure
				}
				return nil
			}, func() { t.Error("failed batch published") })
			b.window = time.Hour
			var work []*batchWork
			for i := range 3 {
				w, err := b.enqueue(t.Context(), 1, 1, func(ctx context.Context) error {
					effects(ctx).live = true
					effects(ctx).after = append(effects(ctx).after, func() { t.Error("failed batch effect") })
					if appendFailure && i == 1 {
						return failure
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				work = append(work, w)
			}
			if err := b.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, w := range work {
				if err := awaitBatch(t, b, w); !errors.Is(err, failure) {
					t.Fatalf("false success: %v", err)
				}
			}
		})
	}
}

// Selected work is still cancellable until its own callback starts, even if
// another export in the same group is blocked on storage/maintenance.
func TestSharedBatchCancelSelectedWork(t *testing.T) {
	b := newIngestBatcher(func(context.Context, bool) error { return nil }, func() {})
	b.window = time.Hour
	entered, release := make(chan struct{}), make(chan struct{})
	first, err := b.enqueue(t.Context(), batchRows-1, 1, func(context.Context) error { close(entered); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	second, err := b.enqueue(ctx, 1, 1, func(context.Context) error { t.Error("canceled selected work executed"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	if err := awaitBatch(t, b, second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	close(release)
	if err := awaitBatch(t, b, first); err != nil {
		t.Fatal(err)
	}
}
