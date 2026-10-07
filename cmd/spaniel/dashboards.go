package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"github.com/zfogg/spaniel/internal/storage"
)

// loadDashboardDefinitions imports every YAML definition deterministically by
// path, so restart updates an existing file-backed dashboard instead of making
// a duplicate. Missing default directories are intentionally empty.
func loadDashboardDefinitions(store *storage.DB, dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read dashboards directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".yaml" || filepath.Ext(entry.Name()) == ".yml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read dashboard %s: %w", path, err)
		}
		definition, err := dashboardconfig.Parse(data)
		if err != nil {
			return fmt.Errorf("parse dashboard %s: %w", path, err)
		}
		sum := sha256.Sum256([]byte(path))
		dashboard, err := definition.Dashboard(fmt.Sprintf("file-%x", sum[:16]))
		if err != nil {
			return fmt.Errorf("convert dashboard %s: %w", path, err)
		}
		for _, panel := range dashboard.Panels {
			if err := storage.ValidateReadOnlySQL(panel.QuerySQL); err != nil {
				return fmt.Errorf("dashboard %s panel %q: %w", path, panel.Title, err)
			}
		}
		if err := store.ReplaceDashboardDefinition(dashboard); err != nil {
			return fmt.Errorf("import dashboard %s: %w", path, err)
		}
	}
	return nil
}
