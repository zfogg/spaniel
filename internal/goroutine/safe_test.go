package goroutine_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/goroutine"
)

func TestGo_PanicDoesNotCrash(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	goroutine.Go(func() {
		defer wg.Done()
		panic("deliberate test panic")
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// pass — goroutine recovered and exited cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not return after panic (timeout)")
	}
}

func TestGo_PanicLogsMessage(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	h := slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelError})

	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	goroutine.Go(func() {
		panic("test-panic-value")
	}, "subsystem", "test-suite")

	// Poll until the log line appears or 3s elapses.  The recover+log defer
	// inside goroutine.Go runs AFTER fn's defers unwind, so there is an
	// unavoidable delay between the panic and the log record being written.
	deadline := time.Now().Add(3 * time.Second)
	var logged string
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		logged = buf.String()
		mu.Unlock()
		if strings.Contains(logged, "test-panic-value") {
			break
		}
	}
	if !strings.Contains(logged, "test-panic-value") {
		t.Errorf("expected log to contain panic value; got: %s", logged)
	}
	if !strings.Contains(logged, "panic recovered") {
		t.Errorf("expected log to contain 'panic recovered'; got: %s", logged)
	}
}

// lockedWriter wraps an io.Writer with a mutex so the slog handler and test
// reader don't race.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

func TestGo_NoPanicRunsNormally(t *testing.T) {
	ran := make(chan bool, 1)
	goroutine.Go(func() { ran <- true })
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not run (timeout)")
	}
}
