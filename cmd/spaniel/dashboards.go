package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"github.com/zfogg/spaniel/internal/storage"
	"gopkg.in/yaml.v3"
)

// loadDashboardDefinitions reconciles the dashboard directory as declarative
// state. ID-less files receive a UUID in place so the same object is recreated
// on a fresh machine, updated after edits, and removed when its file is removed.
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
	type loadedDashboard struct {
		dashboard *storage.Dashboard
		path      string
		contents  []byte
		modified  bool
	}
	loaded := make([]loadedDashboard, 0, len(names))
	seen := make(map[string]string, len(names))
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
		modified := definition.ID == ""
		if modified {
			definition.ID = uuid.NewString()
			data, err = yaml.Marshal(definition)
			if err != nil {
				return fmt.Errorf("add id to dashboard %s: %w", path, err)
			}
		}
		if previous, ok := seen[definition.ID]; ok {
			return fmt.Errorf("dashboard files %s and %s declare duplicate id %q", previous, path, definition.ID)
		}
		seen[definition.ID] = path
		dashboard, err := definition.Dashboard(definition.ID)
		if err != nil {
			return fmt.Errorf("convert dashboard %s: %w", path, err)
		}
		for _, panel := range dashboard.Panels {
			if err := storage.ValidateReadOnlySQL(panel.QuerySQL); err != nil {
				return fmt.Errorf("dashboard %s panel %q: %w", path, panel.Title, err)
			}
		}
		contents := sha256.Sum256(data)
		dashboard.SourceFile = path
		dashboard.SourceHash = fmt.Sprintf("%x", contents[:])
		dashboard.SourceFileModified = modified
		loaded = append(loaded, loadedDashboard{dashboard: dashboard, path: path, contents: data, modified: modified})
	}

	for _, item := range loaded {
		if item.modified {
			if err := writeGeneratedYAMLID(item.path, item.contents); err != nil {
				return fmt.Errorf("write generated dashboard id to %s: %w", item.path, err)
			}
		}
	}
	for _, item := range loaded {
		if existing, err := store.GetDashboard(item.dashboard.ID); err == nil && existing.SourceHash == item.dashboard.SourceHash && existing.SourceFile == item.dashboard.SourceFile {
			continue
		}
		if err := store.ReplaceDashboardDefinition(item.dashboard); err != nil {
			return fmt.Errorf("import dashboard %s: %w", item.path, err)
		}
	}
	existing, err := store.ListDashboards()
	if err != nil {
		return fmt.Errorf("list dashboards for directory reconciliation: %w", err)
	}
	for _, dashboard := range existing {
		if dashboard.SourceFile != "" {
			if _, present := seen[dashboard.ID]; !present {
				if err := store.DeleteDashboard(dashboard.ID); err != nil {
					return fmt.Errorf("remove dashboard whose YAML file was deleted %s: %w", dashboard.SourceFile, err)
				}
			}
		}
	}
	return nil
}
