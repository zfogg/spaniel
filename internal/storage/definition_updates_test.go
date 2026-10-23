package storage

import (
	"path/filepath"
	"testing"
)

func TestDefinitionSeenRevision(t *testing.T) {
	db := openTestDB(t)
	for _, kind := range []string{"dashboard", "alert"} {
		t.Run(kind, func(t *testing.T) {
			check := func(hash string, revision int64, want bool) {
				t.Helper()
				got, err := db.DefinitionUpdated(kind, "shared-id", hash, revision)
				if err != nil || got != want {
					t.Fatalf("updated(%s,%d) = %v, %v; want %v", hash, revision, got, err, want)
				}
			}
			check("", 0, false)
			check("first", 1, true)
			check("first", 1, true) // Listing/background reads must not acknowledge.
			if err := db.MarkDefinitionSeen(kind, "shared-id", "first", 1); err != nil {
				t.Fatal(err)
			}
			check("first", 1, false)
			check("second", 2, true)
			// A late read of an older revision cannot clear a newer edit.
			if err := db.MarkDefinitionSeen(kind, "shared-id", "first", 1); err != nil {
				t.Fatal(err)
			}
			check("second", 2, true)
			check("first", 3, true) // Reverting file contents is still a new revision.
		})
	}
}

func TestDefinitionSeenSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDefinitionSeen("dashboard", "persisted", "hash", 1); err != nil {
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
	updated, err := db.DefinitionUpdated("dashboard", "persisted", "hash", 1)
	if err != nil || updated {
		t.Fatalf("seen revision was lost: %v %v", updated, err)
	}
}
