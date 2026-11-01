package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zfogg/spaniel/internal/model"
)

func TestPhysicalPruneIncludesInboxAndAlertHistory(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	dashboard, err := d.CreateDashboard("preserve saved definitions", "")
	if err != nil {
		t.Fatal(err)
	}
	// No spans/logs/metrics: history alone must remain evictable, as on Pi5.
	for i := range 2000 {
		buf := make([]byte, 2048)
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		payload := hex.EncodeToString(buf)
		if err := d.query.NotificationRecord.Create(&model.NotificationRecord{ID: fmt.Sprint(i), CreatedAt: int64(i + 1), Body: payload}); err != nil {
			t.Fatal(err)
		}
		if err := d.query.AlertEvent.Create(&model.AlertEvent{ID: fmt.Sprint(i), CreatedAt: int64(i + 1), Detail: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	breakdown, err := d.GetStorageBreakdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notification_records", "alert_events"} {
		found := false
		for _, table := range breakdown.Tables {
			if table.Name == name && table.RowCount == 2000 && table.ApproxBytes > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("storage report hides %s", name)
		}
	}
	target := d.FileSize() * 7 / 10
	res, err := d.PrunePhysical(RetentionConfig{MaxDBSizeBytes: target}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedBySize == 0 || d.FileSize() > target {
		t.Fatalf("history did not reclaim storage: %+v target=%d", res, target)
	}
	for _, table := range []string{"notification_records", "alert_events"} {
		var count, oldest, newest int64
		if err := d.SQL().QueryRow("SELECT count(*), min(created_at), max(created_at) FROM "+table).Scan(&count, &oldest, &newest); err != nil {
			t.Fatal(err)
		}
		if count == 0 || newest != 2000 || oldest != 2001-count {
			t.Fatalf("%s lost newest/non-prefix history: %d %d %d", table, count, oldest, newest)
		}
	}
	if _, err := d.GetDashboard(dashboard.ID); err != nil {
		t.Fatal("saved dashboard was pruned:", err)
	}
}

func TestRetentionHistoryAndOrphanFindingsShareOldestQueue(t *testing.T) {
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO notification_records(id, source, source_id, severity, title, dedupe_key, created_at) VALUES ('old', 'issue', '', '', '', '', 1), ('new', 'issue', '', '', '', '', 200)`,
		`INSERT INTO alert_events(id, rule_id, group_key, kind, state, created_at) VALUES ('old', '', '', '', '', 2), ('new', '', '', '', '', 201)`,
		`INSERT INTO trace_issues(id, session_id, trace_id, kind, fingerprint, count, wasted_ns, created_at) VALUES ('orphan', 'gone', 'gone', '', '', 1, 0, 3)`,
		`INSERT INTO lint_warnings(session_id, trace_id, span_id, created_at) VALUES ('gone', 'gone', 'gone', 4)`,
		`INSERT INTO spans(session_id, trace_id, span_id, start_ns, end_ns) VALUES ('active', 'new', 'new', 100, 101)`,
	} {
		if _, err := d.SQL().Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		if err := d.withMaintenance(func(isolated *DB) error {
			_, err := isolated.deleteOldestTelemetryBatch("active")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{"notification_records": 1, "alert_events": 1, "trace_issues": 0, "lint_warnings": 0, "spans": 1} {
		var got int
		if err := d.SQL().QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s=%d want=%d", table, got, want)
		}
	}
}
