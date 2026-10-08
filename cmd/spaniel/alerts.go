package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/storage"
	"os"
	"path/filepath"
	"sort"
)

// loadAlertDefinitions loads YAML definitions into the alert store at startup
// and when Settings requests a reload.
func loadAlertDefinitions(store *storage.DB, dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
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
			return err
		}
		definition, err := alertconfig.Parse(data)
		if err != nil {
			return fmt.Errorf("parse alert %s: %w", path, err)
		}
		identity := sha256.Sum256([]byte(path))
		rule, err := definition.Alert(fmt.Sprintf("file-%x", identity[:16]))
		if err != nil {
			return err
		}
		if err = storage.ValidateReadOnlySQL(rule.QuerySQL); err != nil {
			return err
		}
		contents := sha256.Sum256(data)
		rule.SourceFile = path
		rule.SourceHash = fmt.Sprintf("%x", contents[:])
		if err = store.ReplaceAlertDefinition(rule); err != nil {
			return err
		}
	}
	return nil
}
