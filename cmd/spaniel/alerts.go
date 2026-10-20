package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/storage"
	"gopkg.in/yaml.v3"
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
	type loadedAlert struct {
		rule     *storage.AlertRule
		path     string
		contents []byte
		modified bool
	}
	loaded := make([]loadedAlert, 0, len(names))
	seen := make(map[string]string, len(names))
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
		modified := definition.ID == ""
		if modified {
			definition.ID = uuid.NewString()
			data, err = yaml.Marshal(definition)
			if err != nil {
				return fmt.Errorf("add id to alert %s: %w", path, err)
			}
		}
		if previous, ok := seen[definition.ID]; ok {
			return fmt.Errorf("alert files %s and %s declare duplicate id %q", previous, path, definition.ID)
		}
		seen[definition.ID] = path
		rule, err := definition.Alert(definition.ID)
		if err != nil {
			return err
		}
		if err = storage.ValidateReadOnlySQL(rule.QuerySQL); err != nil {
			return err
		}
		contents := sha256.Sum256(data)
		rule.SourceFile = path
		rule.SourceHash = fmt.Sprintf("%x", contents[:])
		rule.SourceFileModified = modified
		loaded = append(loaded, loadedAlert{rule: rule, path: path, contents: data, modified: modified})
	}
	for _, item := range loaded {
		if item.modified {
			if err := writeGeneratedYAMLID(item.path, item.contents); err != nil {
				return fmt.Errorf("write generated alert id to %s: %w", item.path, err)
			}
		}
	}
	for _, item := range loaded {
		if existing, err := store.GetAlertRule(item.rule.ID); err == nil && existing.SourceHash == item.rule.SourceHash && existing.SourceFile == item.rule.SourceFile {
			continue
		}
		if err := store.ReplaceAlertDefinition(item.rule); err != nil {
			return fmt.Errorf("import alert %s: %w", item.path, err)
		}
	}
	existing, err := store.ListAlertRules()
	if err != nil {
		return fmt.Errorf("list alerts for directory reconciliation: %w", err)
	}
	for _, rule := range existing {
		if rule.SourceFile != "" {
			if _, present := seen[rule.ID]; !present {
				if err := store.DeleteAlertRule(rule.ID); err != nil {
					return fmt.Errorf("remove alert whose YAML file was deleted %s: %w", rule.SourceFile, err)
				}
			}
		}
	}
	return nil
}
