package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"github.com/zfogg/spaniel/internal/storage"
)

func TestLoadDashboardDefinitionsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", "dashboard.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dashboard.yml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dir, "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 2; i++ {
		if err := loadDashboardDefinitions(store, dir); err != nil {
			t.Fatal(err)
		}
	}
	dashboards, err := store.ListDashboards()
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboards) != 1 || len(dashboards[0].Variables) != 1 || len(dashboards[0].Panels) != 1 {
		t.Fatalf("unexpected imported dashboard: %#v", dashboards)
	}
	if dashboards[0].SourceFile == "" || !dashboards[0].SourceFileModified {
		t.Fatalf("dashboard provenance = %#v", dashboards[0])
	}
	written, err := os.ReadFile(filepath.Join(dir, "dashboard.yml"))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := dashboardconfig.Parse(written)
	if err != nil {
		t.Fatal(err)
	}
	if definition.ID == "" {
		t.Fatal("loader did not write an id into dashboard YAML")
	}
	updatedAt := dashboards[0].UpdatedAt
	if err := loadDashboardDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.GetDashboard(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UpdatedAt != updatedAt {
		t.Fatalf("unchanged file was rewritten: updated_at %d -> %d", updatedAt, reloaded.UpdatedAt)
	}
	if err := store.AcknowledgeDashboardSourceFile(definition.ID); err != nil {
		t.Fatal(err)
	}
	written = []byte(strings.Replace(string(written), "YAML browser check", "YAML browser check updated", 1))
	if err := os.WriteFile(filepath.Join(dir, "dashboard.yml"), written, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDashboardDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	reloaded, err = store.GetDashboard(definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Name != "YAML browser check updated" || reloaded.SourceFileModified {
		t.Fatalf("updated dashboard = %#v", reloaded)
	}

	fresh, err := storage.Open(filepath.Join(dir, "fresh.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := loadDashboardDefinitions(fresh, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.GetDashboard(definition.ID); err != nil {
		t.Fatalf("fresh database did not recreate stable dashboard id: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "dashboard.yml")); err != nil {
		t.Fatal(err)
	}
	if err := loadDashboardDefinitions(store, dir); err != nil {
		t.Fatal(err)
	}
	dashboards, err = store.ListDashboards()
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboards) != 0 {
		t.Fatalf("dashboard remained after YAML deletion: %#v", dashboards)
	}
}

func TestLoadDashboardDefinitionsRejectsDuplicateIDsBeforeImport(t *testing.T) {
	dir := t.TempDir()
	definition := "version: 1\nid: duplicate-dashboard\nname: One\npanels: []\n"
	for _, name := range []string{"a.yaml", "b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(definition), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.Open(filepath.Join(dir, "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := loadDashboardDefinitions(store, dir); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
	dashboards, err := store.ListDashboards()
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboards) != 0 {
		t.Fatalf("duplicate preflight partially imported dashboards: %#v", dashboards)
	}
}
