package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/yamlquery"
)

func TestLoadDefinitionsFormatsExistingIDAndPreservesHash(t *testing.T) {
	for _, kind := range []string{"alert", "dashboard"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "definition.yaml")
			source := "# retained\nversion: 1\nid: fixed\nname: Test\n"
			if kind == "alert" {
				source += "query: SELECT 1 AS value\ncondition:\n  kind: threshold\n  operator: '>'\n  threshold: 0\npending_for: 0s\ncooldown: 0s\nseverity: warning\n"
			} else {
				source += "panels:\n- title: Count\n  display_type: single_value\n  position: 0\n  query: SELECT 1 AS value\n"
			}
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(filepath.Join(dir, "test.duckdb"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for i := 0; i < 2; i++ {
				if kind == "alert" {
					err = loadAlertDefinitions(store, dir)
				} else {
					err = loadDashboardDefinitions(store, dir)
				}
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(data, []byte("# retained")) {
					t.Fatal("lost comment")
				}
				if _, changed, err := yamlquery.Format(data); err != nil || changed {
					t.Fatalf("not formatted: %v %v", changed, err)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(data))
				if kind == "alert" {
					rule, err := store.GetAlertRule("fixed")
					if err != nil || rule.SourceHash != hash {
						t.Fatalf("hash mismatch: %v", err)
					}
				} else {
					dash, err := store.GetDashboard("fixed")
					if err != nil || dash.SourceHash != hash {
						t.Fatalf("hash mismatch: %v", err)
					}
				}
			}
		})
	}
}
