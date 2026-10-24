package api

import (
	"crypto/sha256"
	"fmt"
	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchDashboardMetadataWritesYAMLAndReloads(t *testing.T) {
	_, db := setupRouter(t)
	path := filepath.Join(t.TempDir(), "dashboard.yaml")
	data := []byte("version: 1\nid: metadata\nname: Before\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reload := func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		definition, err := dashboardconfig.Parse(data)
		if err != nil {
			return err
		}
		dashboard, err := definition.Dashboard(definition.ID)
		if err != nil {
			return err
		}
		dashboard.SourceFile, dashboard.SourceHash = path, fmt.Sprintf("%x", sha256.Sum256(data))
		return db.ReplaceDashboardDefinition(dashboard)
	}
	if err := reload(); err != nil {
		t.Fatal(err)
	}
	handler := NewRouterFull(db, nil, nil, nil, &SettingsService{ReloadDashboards: reload}, nil, nil, nil)
	response := do(t, handler, http.MethodPatch, "/api/dashboards/metadata", map[string]string{"name": "After", "description": "Edited description"})
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "name: After") || !strings.Contains(string(saved), "description: Edited description") {
		t.Fatalf("source not saved: %s", saved)
	}
	if err := reload(); err != nil {
		t.Fatal(err)
	}
	dashboard, err := db.GetDashboard("metadata")
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Name != "After" || dashboard.Description != "Edited description" {
		t.Fatalf("reload lost edit: %#v", dashboard)
	}
	if err := os.WriteFile(path, append(saved, []byte("# external edit\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	response = do(t, handler, http.MethodPatch, "/api/dashboards/metadata", map[string]string{"name": "Stale"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale write: %d %s", response.Code, response.Body.String())
	}
}
