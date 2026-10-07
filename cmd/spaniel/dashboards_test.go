package main

import (
	"os"
	"path/filepath"
	"testing"

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
}
