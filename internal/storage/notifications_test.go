package storage

import (
	"fmt"
	"path/filepath"
	"testing"
)

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
