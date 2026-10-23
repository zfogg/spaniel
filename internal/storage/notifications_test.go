package storage

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFindingNotificationDeduplicationSurvivesReanalysisAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"issue", "lint", "alert"} {
		if err := db.RecordNotification(&NotificationRecord{Source: source, DedupeKey: source, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := db.ListNotifications("issue", 1, 30)
	if err != nil || len(items) != 1 {
		t.Fatalf("initial issue: %v %v", items, err)
	}
	if err := db.MarkNotificationRead(items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for _, source := range []string{"issue", "lint", "alert"} {
				if err := db.RecordNotification(&NotificationRecord{Source: source, DedupeKey: source, CreatedAt: int64(time.Hour)}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	for _, source := range []string{"issue", "lint", "alert"} {
		items, total, err := db.ListNotifications(source, 1, 30)
		want := int64(1)
		if source == "alert" {
			want = 2 // A new alert lifecycle delivery is still allowed.
		}
		if err != nil || total != want {
			t.Fatalf("%s total=%d want=%d err=%v", source, total, want, err)
		}
		if source == "issue" && items[0].AcknowledgedAt == nil {
			t.Fatal("reanalysis lost the operator acknowledgement")
		}
	}
	if err := db.RecordNotification(&NotificationRecord{Source: "issue", DedupeKey: "new-finding"}); err != nil {
		t.Fatal(err)
	}
	_, total, err := db.ListNotifications("issue", 1, 30)
	if err != nil || total != 2 {
		t.Fatalf("new finding suppressed: total=%d err=%v", total, err)
	}
}

func TestNotificationHistoryPaginationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 36; i++ {
		if err := db.RecordNotification(&NotificationRecord{ID: fmt.Sprintf("n-%02d", i), Source: "alert", Title: "Browser alert", CreatedAt: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertTraceIssue(&TraceIssue{ID: "issue-one", TraceID: "trace", Kind: "n_plus_one", Count: 4}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := db.ListNotifications("alert", 1, 35)
	if err != nil || total != 36 || len(rows) != 35 || rows[0].ID != "n-35" {
		t.Fatalf("first page: %d %d %v", len(rows), total, err)
	}
	rows, total, err = db.ListNotifications("alert", 2, 35)
	if err != nil || total != 36 || len(rows) != 1 || rows[0].ID != "n-00" {
		t.Fatalf("second page: %#v %d %v", rows, total, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, total, err = db.ListNotifications("issue", 1, 35)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Link != "/traces/trace" {
		t.Fatalf("persisted issue notification: %#v %d %v", rows, total, err)
	}
}
