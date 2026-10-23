package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPhysicalPruneMeasuresFilesAndRestoresServingHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "physical.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.CreateSession("physical", false)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	for i := range 1800 {
		if _, err = rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		if err = d.AppendSpan(&Span{SessionID: s.ID, TraceID: fmt.Sprintf("trace-%d", i), SpanID: fmt.Sprintf("%016x", i), StartNs: int64(i + 1), EndNs: int64(i + 2), Attributes: `{"payload":"` + hex.EncodeToString(buf) + `"}`, Resource: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err = d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	before := d.FileSize()
	target := before * 6 / 10
	// Hold a generated-query/context clone across replacement, just like an API
	// request. Both it and the batcher must use the newly opened physical file.
	clone := d.WithContext(t.Context())
	res, err := d.PrunePhysical(RetentionConfig{MaxDBSizeBytes: target}, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Full() {
		t.Fatal("successful proactive pruning must not declare storage full")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	physical := info.Size() + d.WALSize()
	if physical > target || res.FinalDBSizeBytes != physical || res.DeletedBySize == 0 {
		t.Fatalf("before=%d target=%d stat+wal=%d result=%+v", before, target, physical, res)
	}
	var count, oldest, newest int64
	if err = clone.gorm.Raw("SELECT count(*),min(start_ns),max(start_ns) FROM spans").Row().Scan(&count, &oldest, &newest); err != nil {
		t.Fatal(err)
	}
	if count == 0 || newest != 1800 || oldest != 1801-count {
		t.Fatalf("lost newest/non-prefix eviction: count=%d oldest=%d newest=%d", count, oldest, newest)
	}
	if err = d.AppendSpan(&Span{SessionID: s.ID, TraceID: "after", SpanID: "after", StartNs: 2000, EndNs: 2001}); err != nil {
		t.Fatal(err)
	}
	if err = d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err = clone.gorm.Raw("SELECT count(*) FROM spans WHERE span_id='after'").Row().Scan(&n); err != nil || n != 1 {
		t.Fatalf("restored appenders/clone: n=%d err=%v", n, err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.SQL().QueryRow("SELECT count(*) FROM spans WHERE span_id='after'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("restart persistence n=%d err=%v", n, err)
	}
}

func TestNativePhysicalCopyPreservesViewsAndQualifiedIndexes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "native.duckdb")
	d, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.SQL().Exec(`CREATE TABLE retained (id INTEGER PRIMARY KEY, value VARCHAR DEFAULT 'kept');
		CREATE INDEX retained_value ON native.main.retained(value);
		INSERT INTO retained(id) VALUES (7);
		CREATE VIEW retained_view AS SELECT * FROM retained`); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(t.TempDir(), "native.duckdb")
	if err = copyPhysicalDatabase(source, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(candidate), "export")); !os.IsNotExist(err) {
		t.Fatal("ordinary retention copied through intermediate Parquet")
	}
	copy, err := Open(candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var value string
	if err = copy.SQL().QueryRow("SELECT value FROM retained_view WHERE id=7").Scan(&value); err != nil || value != "kept" {
		t.Fatalf("view/default lost: %q %v", value, err)
	}
	var indexes int
	if err = copy.SQL().QueryRow("SELECT count(*) FROM duckdb_indexes() WHERE index_name='retained_value'").Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("qualified index lost: count=%d err=%v", indexes, err)
	}
	if _, err = copy.SQL().Exec("INSERT INTO retained(id) VALUES (7)"); err == nil {
		t.Fatal("primary key constraint lost")
	}
}

func TestPhysicalRewriteReclaimsHolesWithoutEvictingSurvivors(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "holes.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err = d.SQL().Exec("CREATE SEQUENCE preserved_seq START 40; CREATE TABLE preserved_defaults (id BIGINT DEFAULT nextval('preserved_seq') PRIMARY KEY, value VARCHAR UNIQUE); INSERT INTO preserved_defaults(value) VALUES ('one'); CREATE VIEW preserved_view AS SELECT * FROM preserved_defaults"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.SQL().Exec("CREATE TABLE retained_config AS SELECT i, repeat(md5(i::VARCHAR),100) payload FROM range(12000) t(i)"); err != nil {
		t.Fatal(err)
	}
	if err = d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	before := d.FileSize()
	if _, err = d.SQL().Exec("DELETE FROM retained_config WHERE i<11000"); err != nil {
		t.Fatal(err)
	}
	res, err := d.PrunePhysical(RetentionConfig{MaxDBSizeBytes: before * 9 / 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize != 0 || d.FileSize() >= before {
		t.Fatalf("unnecessary live eviction/no physical shrink: before=%d after=%d result=%+v", before, d.FileSize(), res)
	}
	var n int
	if err = d.SQL().QueryRow("SELECT count(*) FROM retained_config").Scan(&n); err != nil || n != 1000 {
		t.Fatalf("preserved custom table n=%d err=%v", n, err)
	}
	if _, err = d.SQL().Exec("INSERT INTO preserved_defaults(value) VALUES ('two')"); err != nil {
		t.Fatal(err)
	}
	if err = d.SQL().QueryRow("SELECT id FROM preserved_view WHERE value='two'").Scan(&n); err != nil || n != 41 {
		t.Fatalf("sequence/default/view did not survive: id=%d err=%v", n, err)
	}
	if _, err = d.SQL().Exec("INSERT INTO preserved_defaults(value) VALUES ('two')"); err == nil {
		t.Fatal("unique constraint lost")
	}
}

func TestPhysicalRewriteWithConcurrentQueries(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "concurrent.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	stop := make(chan struct{})
	failures := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
				}
				var n int64
				if err := d.WithContext(ctx).gorm.Raw("SELECT count(*) FROM sessions").Row().Scan(&n); err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	close(start)
	d.maintenanceMu.Lock()
	err = d.rewritePhysicalLocked()
	d.maintenanceMu.Unlock()
	close(stop)
	wg.Wait()
	close(failures)
	if err != nil {
		t.Fatal(err)
	}
	for e := range failures {
		t.Error(e)
	}
}

func TestPhysicalSwapRecoversBetweenRenames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recover.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.SQL().Exec("CREATE TABLE preserved AS SELECT 42 answer"); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".repack-old"); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err = d.SQL().QueryRow("SELECT answer FROM preserved").Scan(&n); err != nil || n != 42 {
		t.Fatalf("recovery answer=%d err=%v", n, err)
	}
}

// Rapid pressure after a successful low-water recovery must make room for
// live data rather than spending another whole pause copying the same rows.
func TestPhysicalPruneRapidRepeatEvictsBeforeCopy(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "repeat.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seed := func(start int) {
		t.Helper()
		for i := start; i < start+1000; i++ {
			payload := make([]byte, 4096)
			if _, err := rand.Read(payload); err != nil {
				t.Fatal(err)
			}
			if err := d.AppendSpan(&Span{TraceID: fmt.Sprint(i), SpanID: fmt.Sprint(i), StartNs: int64(i + 1), EndNs: int64(i + 2), Attributes: `{"payload":"` + hex.EncodeToString(payload) + `"}`}); err != nil {
				t.Fatal(err)
			}
		}
		if err := d.Checkpoint(); err != nil {
			t.Fatal(err)
		}
	}
	seed(0)
	target := d.FileSize() * 7 / 10
	cfg := RetentionConfig{MaxDBSizeBytes: target, MaxDuration: time.Minute}
	for phase := 0; phase < 12 && d.FileSize() > target; phase++ {
		if _, err := d.PrunePhysical(cfg, ""); err != nil {
			t.Fatal(err)
		}
	}
	if d.FileSize() > target {
		t.Fatal("first recovery did not reach low water")
	}
	// Retain the shared timestamp across request/context clones as in production.
	clone := d.WithContext(context.Background())
	seed(1000)
	if d.FileSize() <= target {
		t.Fatal("refill did not create capacity pressure")
	}
	result, err := clone.PrunePhysical(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedBySize == 0 {
		t.Fatal("rapid repeat copied survivors without evicting oldest telemetry")
	}
	var newest int
	if err := d.SQL().QueryRow("SELECT count(*) FROM spans WHERE start_ns=2000").Scan(&newest); err != nil || newest != 1 {
		t.Fatalf("newest span lost: count=%d err=%v", newest, err)
	}
	if d.Full() {
		t.Fatal("successful repeat proactive phase paused admission")
	}
}
