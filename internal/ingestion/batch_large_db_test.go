package ingestion

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// The routine fixture exercises populated indexes and disk-backed appenders.
// SPANIEL_LARGE_DB_REGRESSION=1 reproduces Ratio's observed million-span,
// roughly 500 MiB database without modifying or copying its live database.
func TestSharedBatchPopulatedDatabase(t *testing.T) {
	rows, target := 10000, int64(16<<20)
	if os.Getenv("SPANIEL_LARGE_DB_REGRESSION") == "1" {
		rows, target = 1000000, 500<<20
	}
	path := filepath.Join(t.TempDir(), "populated.duckdb")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	session, err := db.CreateSession("populated database regression", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	for i := range rows {
		if err := db.AppendSpan(&storage.Span{
			TraceID: fmt.Sprintf("%032x", i/30+1), SpanID: fmt.Sprintf("%016x", i+1),
			ServiceName: "historical", Name: "historical database query", Kind: 3,
			StartNs: int64(i + 1), EndNs: int64(i + 2), DurationNs: 1,
			SessionID: session.ID, Attributes: `{"db.system":"postgresql","db.query.text":"SELECT id FROM positions WHERE account_id = $1"}`, Resource: `{}`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	// Unique deterministic payloads resist dictionary/compression shortcuts;
	// verify live blocks, rather than padding a sparse file or counting WAL only.
	rng := rand.New(rand.NewPCG(42, 17))
	payload := make([]byte, 32<<10)
	for db.UsedSize() < target {
		for range 128 {
			for i := range payload {
				payload[i] = byte(rng.Uint32())
			}
			if err := db.AppendLog(&storage.Log{ServiceName: "historical", SessionID: session.ID, Body: hex.EncodeToString(payload), Attributes: `{}`}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.FlushBatch(); err != nil {
			t.Fatal(err)
		}
		if err := db.Checkpoint(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("fixture: historical_spans=%d used_bytes=%d allocated_bytes=%d", rows, db.UsedSize(), db.FileSize())
	p := NewPipeline(db, ws.NewHub())
	p.SetSelfService("populated-burst") // isolate storage from asynchronous lint work
	defer p.Close(context.Background())
	// Place maintenance at the actual commit boundary, after rows have been
	// appended. Starting it with the clients would usually finish during the
	// one-second collection window and miss the contention being tested.
	rowsReady, checkpointDone := make(chan struct{}), make(chan struct{})
	flush := p.batches.flush
	var ready sync.Once
	p.batches.flush = func(ctx context.Context, instrument bool) error {
		ready.Do(func() { close(rowsReady) })
		<-checkpointDone
		return flush(ctx, instrument)
	}
	const exports, perExport = 100, 3
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, exports+2)
	var wg sync.WaitGroup
	for i := range exports {
		wg.Add(1)
		go func() {
			defer wg.Done()
			td := makeTraces("populated-burst", func(ss ptrace.ScopeSpans) {
				for j := range perExport {
					putSpan(ss, fmt.Sprintf("%032x", rows+i+1), fmt.Sprintf("%016x", rows+i*perExport+j+1), "", "populated database burst", ptrace.SpanKindInternal, nil)
				}
			})
			<-start
			errs <- p.IngestTraces(ctx, td)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-rowsReady
		for range 5 {
			if _, err := db.WithContext(ctx).ListSpans(storage.SpanFilter{SessionID: session.ID, Service: "historical", Limit: 25}); err != nil {
				errs <- err
				return
			}
		}
	}()
	// A populated database also exercises the guarded checkpoint boundary used
	// by the live storage guard. Reads and ingestion must resume without a
	// lost acknowledgment or an appender/connection-pool deadlock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-rowsReady
		err := db.WithContext(ctx).Checkpoint()
		close(checkpointDone)
		errs <- err
	}()
	began := time.Now()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("populated database export/read failed: %v", err)
		}
	}
	if elapsed := time.Since(began); elapsed >= 10*time.Second {
		t.Fatalf("exports exceeded Ratio's 10s acknowledgment budget: %s", elapsed)
	} else {
		t.Logf("100 exports, 5 dashboard reads and a guarded checkpoint completed in %s", elapsed)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for service, want := range map[string]int{"historical": rows, "populated-burst": exports * perExport} {
		var got int
		if err := db.SQL().QueryRowContext(t.Context(), "SELECT count(*) FROM spans WHERE service_name = ?", service).Scan(&got); err != nil || got != want {
			t.Fatalf("reopened %s spans=%d want=%d error=%v", service, got, want, err)
		}
	}
}
