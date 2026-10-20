package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/storage"
)

const alertDirectoryYAML = `version: 1
name: Error count
query: SELECT 1 AS value
condition:
  kind: threshold
  operator: ">"
  threshold: 0
pending_for: 0s
cooldown: 0s
severity: warning
`

func TestLoadAlertDefinitionsWritesStableIDAndReconciles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.yaml")
	if err := os.WriteFile(path, []byte(alertDirectoryYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dir, "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := loadAlertDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := alertconfig.Parse(written)
	if err != nil {
		t.Fatal(err)
	}
	if definition.ID == "" {
		t.Fatal("loader did not write an id into alert YAML")
	}
	rule, err := store.GetAlertRule(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rule.SourceFile != path || !rule.SourceFileModified {
		t.Fatalf("alert provenance = %#v", rule)
	}
	updatedAt := rule.UpdatedAt
	if err := loadAlertDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	rule, err = store.GetAlertRule(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rule.UpdatedAt != updatedAt || !rule.SourceFileModified {
		t.Fatalf("unchanged alert reload = %#v", rule)
	}
	if err := store.AcknowledgeAlertSourceFile(definition.ID); err != nil {
		t.Fatal(err)
	}
	written = []byte(strings.Replace(string(written), "Error count", "Error count updated", 1))
	if err := os.WriteFile(path, written, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadAlertDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	rule, err = store.GetAlertRule(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Name != "Error count updated" || rule.SourceFileModified {
		t.Fatalf("updated alert = %#v", rule)
	}

	fresh, err := storage.Open(filepath.Join(dir, "fresh.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := loadAlertDefinitions(fresh, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.GetAlertRule(definition.ID); err != nil {
		t.Fatalf("fresh database did not recreate stable alert id: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := loadAlertDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	rules, err := store.ListAlertRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("alert remained after YAML deletion: %#v", rules)
	}
}

func TestLoadAlertDefinitionsRejectsDuplicateIDsBeforeImport(t *testing.T) {
	dir := t.TempDir()
	definition := strings.Replace(alertDirectoryYAML, "version: 1", "version: 1\nid: duplicate-alert", 1)
	for _, name := range []string{"a.yaml", "b.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(definition), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.Open(filepath.Join(dir, "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := loadAlertDefinitions(store, dir); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
	rules, err := store.ListAlertRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("duplicate preflight partially imported alerts: %#v", rules)
	}
}
