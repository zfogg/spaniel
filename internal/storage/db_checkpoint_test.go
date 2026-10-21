package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWALDeferralAppliesToFreshConnections(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.WALSize() != 0 {
		t.Fatal("in-memory database reported a WAL file")
	}
	// Remove idle metadata connections, then hold both new leases at once so
	// constructor configuration is checked on distinct fresh connections.
	d.SQL().SetMaxIdleConns(0)
	var connections []*sql.Conn
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	for range 2 {
		conn, err := d.SQL().Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		var threshold, alias string
		var locked bool
		if err := conn.QueryRowContext(t.Context(), `SELECT current_setting('checkpoint_threshold'), current_setting('wal_autocheckpoint'), current_setting('lock_configuration')`).Scan(&threshold, &alias, &locked); err != nil {
			t.Fatal(err)
		}
		// DuckDB formats the configured decimal 1 TB as binary GiB.
		if threshold != "931.3 GiB" || alias != threshold || !locked {
			t.Fatalf("unexpected checkpoint configuration: threshold=%q alias=%q locked=%v", threshold, alias, locked)
		}
	}
}

func TestWALDeferralPreservesReplayAndShutdown(t *testing.T) {
	const rows = 1024
	const childEnv = "SPANIEL_WAL_RECOVERY_TEST_PATH"
	path := os.Getenv(childEnv)
	if path != "" {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately do not register Close: process exit must leave committed WAL.
		var lastBody string
		for i := range rows {
			payload := make([]byte, 16*1024)
			if _, err := rand.Read(payload); err != nil {
				t.Fatal(err)
			}
			lastBody = fmt.Sprintf("%d:", i) + hex.EncodeToString(payload)
			if err := d.AppendLog(&Log{TimestampNs: int64(i + 1), Body: lastBody, Attributes: `{}`, ServiceName: "wal-replay-test"}); err != nil {
				t.Fatal(err)
			}
			if (i+1)%128 == 0 {
				if err := d.FlushBatch(); err != nil {
					t.Fatal(err)
				}
			}
		}
		if wal := d.WALSize(); wal <= 16*1024*1024 {
			t.Fatalf("native checkpoint was not deferred beyond 16 MiB: WAL=%d", wal)
		}
		if err := os.WriteFile(path+".sentinel", []byte(lastBody), 0600); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	path = filepath.Join(t.TempDir(), "spaniel.duckdb")
	child := exec.Command(os.Args[0], "-test.run=^TestWALDeferralPreservesReplayAndShutdown$", "-test.count=1")
	child.Env = append(os.Environ(), childEnv+"="+path)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("WAL writer: %v\n%s", err, output)
	}
	wal, err := os.Stat(path + ".wal")
	if err != nil {
		t.Fatal(err)
	}
	if wal.Size() <= 16*1024*1024 {
		t.Fatalf("missing uncheckpointed WAL: %d", wal.Size())
	}
	lastBody, err := os.ReadFile(path + ".sentinel")
	if err != nil {
		t.Fatal(err)
	}
	verify := func(db *DB) {
		t.Helper()
		var count int
		if err := db.SQL().QueryRow("SELECT count(*) FROM logs").Scan(&count); err != nil {
			t.Fatal(err)
		}
		var body string
		if err := db.SQL().QueryRow("SELECT body FROM logs WHERE timestamp_ns=?", rows).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if count != rows || body != string(lastBody) {
			t.Fatalf("WAL data changed: rows=%d want=%d sentinel_matches=%v", count, rows, body == string(lastBody))
		}
	}
	recovered, err := Open(path)
	if err != nil {
		t.Fatalf("recover after abrupt process exit: %v", err)
	}
	verify(recovered)
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after ordinary shutdown: %v", err)
	}
	defer reopened.Close()
	verify(reopened)
}
