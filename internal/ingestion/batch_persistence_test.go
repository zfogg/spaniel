package ingestion

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// Mix actual signal types across more requests than the old processing-slot
// count. Check visibility from the commit notification, then reopen the DB.
func TestSharedBatchMixedSignalsPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch.duckdb")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	sess, err := db.CreateSession("batch", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(sess.ID, sess.Label)
	p := NewPipeline(db, ws.NewHub())
	p.SetSelfService("batch-test") // isolate persistence from diagnostic work
	p.batches.window = time.Hour
	var notifications atomic.Int64
	p.SetLiveNotifier(func() { notifications.Add(1) })
	var wg sync.WaitGroup
	errs := make(chan error, 90)
	for i := range 90 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch i % 3 {
			case 0:
				td := makeTraces("batch-test", func(ss ptrace.ScopeSpans) {
					putSpan(ss, "a", fmt.Sprint(i), "", "batch span", ptrace.SpanKindInternal, nil)
				})
				errs <- p.IngestTraces(t.Context(), td)
			case 1:
				ld := plog.NewLogs()
				rl := ld.ResourceLogs().AppendEmpty()
				rl.Resource().Attributes().PutStr("service.name", "batch-test")
				rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("batch log")
				errs <- p.IngestLogs(t.Context(), ld)
			case 2:
				md := pmetric.NewMetrics()
				rm := md.ResourceMetrics().AppendEmpty()
				rm.Resource().Attributes().PutStr("service.name", "batch-test")
				m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
				m.SetName("batch.metric")
				m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(float64(i))
				errs <- p.IngestMetrics(t.Context(), md)
			}
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.batches.mu.Lock()
		n := len(p.batches.pending)
		p.batches.mu.Unlock()
		if n == 90 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exports did not enter shared queue; processing slots may be retained")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		for _, table := range []string{"spans", "logs", "metrics"} {
			var n int
			if err := db.SQL().QueryRowContext(context.Background(), "SELECT count(*) FROM "+table+" WHERE session_id = ?", sess.ID).Scan(&n); err != nil || n != 30 {
				t.Fatalf("%s: count=%d error=%v", table, n, err)
			}
		}
	}
	check()
	if notifications.Load() != 0 {
		t.Fatal("self telemetry unexpectedly published live state")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check()
}
