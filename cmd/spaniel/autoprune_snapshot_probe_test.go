package main

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestAutoPruneExistingSnapshotDiagnosis(t *testing.T) {
	path := os.Getenv("SPANIEL_REPRO_DB")
	if path == "" {
		t.Skip("requires a disposable consistent database snapshot")
	}
	d, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	t.Logf("initial live=%d file=%d", d.UsedSize(), d.FileSize())
	for _, table := range []string{"spans", "logs", "metrics", "sessions", "span_events", "span_links", "lint_warnings", "trace_issues"} {
		var estimated, live int64
		if err := d.SQL().QueryRow("SELECT estimated_size FROM duckdb_tables() WHERE table_name=? AND schema_name='main'", table).Scan(&estimated); err != nil {
			t.Fatal(err)
		}
		if err := d.SQL().QueryRow("SELECT count(*) FROM " + table).Scan(&live); err != nil {
			t.Fatal(err)
		}
		t.Logf("table=%s estimated=%d live=%d", table, estimated, live)
	}
	if os.Getenv("SPANIEL_REPRO_GUARD") == "1" {
		for i := range 16 {
			started := time.Now()
			checkStorageGuard(d, newStorageGuardPolicy(500<<20, true))
			t.Logf("guard pass=%d duration=%s live=%d full=%v", i, time.Since(started), d.UsedSize(), d.Full())
			if !d.Full() && d.UsedSize() < 500<<20 {
				break
			}
		}
		if d.Full() || d.UsedSize() >= 500<<20 {
			t.Fatalf("automatic guard never recovered: live=%d full=%v", d.UsedSize(), d.Full())
		}
		session, err := d.CreateSession("auto-prune-recovery-probe", false)
		if err != nil {
			t.Fatal(err)
		}
		d.SetActiveSession(session.ID, session.Label)
		for cycle := range 2 {
			added := 0
			for ; added < 1024 && d.UsedSize() < 500<<20; added++ {
				body := make([]byte, 64<<10)
				if _, err := rand.Read(body); err != nil {
					t.Fatal(err)
				}
				if err := d.AppendLog(&storage.Log{SessionID: session.ID, TimestampNs: time.Now().UnixNano(), Body: base64.StdEncoding.EncodeToString(body), Attributes: `{}`}); err != nil {
					t.Fatal(err)
				}
				if err := d.FlushBatch(); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("cycle=%d added=%d live=%d", cycle, added, d.UsedSize())
			if d.UsedSize() < 500<<20 {
				t.Fatal("fixture did not cross hard cap")
			}
			for pass := range 20 {
				for d.RetentionBackingOff() {
					time.Sleep(100 * time.Millisecond)
				}
				started := time.Now()
				checkStorageGuard(d, newStorageGuardPolicy(500<<20, true))
				t.Logf("cycle=%d pass=%d duration=%s live=%d full=%v", cycle, pass, time.Since(started), d.UsedSize(), d.Full())
				if !d.Full() && d.UsedSize() < 500<<20 {
					break
				}
			}
			if d.Full() || d.UsedSize() >= 500<<20 {
				t.Fatalf("automatic guard did not recover cycle %d", cycle)
			}
		}
		return
	}
	for i := range 12 {
		started := time.Now()
		res, err := d.Prune(storage.RetentionConfig{MaxDBSizeBytes: 350 << 20, MaxDuration: 8 * time.Second}, d.ActiveSessionID())
		t.Logf("pass=%d duration=%s live=%d result=%+v error=%v", i, time.Since(started), d.UsedSize(), res, err)
		if err != nil || d.UsedSize() < 400<<20 {
			break
		}
	}
	started := time.Now()
	res, err := d.Prune(storage.RetentionConfig{MaxDBSizeBytes: 350 << 20}, d.ActiveSessionID())
	t.Logf("unbounded duration=%s live=%d result=%+v error=%v", time.Since(started), d.UsedSize(), res, err)
	if err != nil {
		t.Fatal(err)
	}
	if d.UsedSize() >= 500<<20 {
		t.Fatalf("still above hard cap: %d", d.UsedSize())
	}
}
