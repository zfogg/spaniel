package dashboardconfig

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMetadataPreservesPanelsCommentsAndRejectsStaleWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.yaml")
	original := []byte("# retained comment\nversion: 1\nid: stable\nname: Before\npanels:\n  - title: Count\n    display_type: single_value\n    query: SELECT 1 AS value\n    position: 0\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(original))
	if err := WriteMetadata(path, "stable", hash, "After", "New description"); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(saved)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "After" || parsed.Description != "New description" || parsed.ID != "stable" || len(parsed.Panels) != 1 || parsed.Panels[0].Query != "SELECT 1 AS value" || !strings.Contains(string(saved), "# retained comment") {
		t.Fatalf("unexpected document: %s", saved)
	}
	if err := WriteMetadata(path, "stable", hash, "Stale", ""); err == nil {
		t.Fatal("stale save accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(saved) {
		t.Fatal("stale save modified source")
	}
}
