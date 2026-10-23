package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type definitionTestHub struct{ events chan *ws.Event }

func (h *definitionTestHub) HasViewers() bool      { return true }
func (h *definitionTestHub) Broadcast(e *ws.Event) { h.events <- e }
func waitDefinitionEvent(t *testing.T, h *definitionTestHub, kind, status string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-h.events:
			p := e.Payload.(map[string]string)
			if p["kind"] == kind && p["status"] == status {
				return
			}
		case <-timer.C:
			t.Fatalf("missing %s %s event", kind, status)
		}
	}
}

func TestWatchDefinitionDirectories(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "test.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboards, alerts := filepath.Join(root, "dashboards"), filepath.Join(root, "alerts")
	hub := &definitionTestHub{events: make(chan *ws.Event, 100)}
	stop, err := watchDefinitionDirectories(context.Background(), store, hub, dashboards, alerts)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// Startup reconciliation attaches initially absent directories.
	waitDefinitionEvent(t, hub, "dashboard", "updated")
	waitDefinitionEvent(t, hub, "alert", "updated")
	data, err := os.ReadFile(filepath.Join("testdata", "dashboard.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dashboardPath := filepath.Join(dashboards, "dashboard.yaml")
	if err := os.WriteFile(dashboardPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "dashboard", "updating")
	waitDefinitionEvent(t, hub, "dashboard", "updated")
	saved, err := store.ListDashboards()
	if err != nil || len(saved) != 1 {
		t.Fatalf("created dashboards: %v %v", saved, err)
	}
	id := saved[0].ID
	if !saved[0].Updated {
		t.Fatal("new dashboard is not unread")
	}
	data, err = os.ReadFile(dashboardPath) // Includes the stable ID written by Spaniel.
	if err != nil {
		t.Fatal(err)
	}
	// Atomic replacement is how many editors save; watching a file inode loses it.
	replacement := filepath.Join(dashboards, "save.tmp")
	if err := os.WriteFile(replacement, []byte(strings.Replace(string(data), "name:", "name: Reloaded #", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, dashboardPath); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "dashboard", "updated")
	dashboard, err := store.GetDashboard(id)
	if err != nil || dashboard.Name != "Reloaded" {
		t.Fatalf("atomic save: %v %v", dashboard, err)
	}
	if err := os.WriteFile(dashboardPath, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "dashboard", "error")
	dashboard, err = store.GetDashboard(id)
	if err != nil || dashboard.Name != "Reloaded" {
		t.Fatal("invalid YAML replaced the last valid dashboard")
	}
	if err := os.Remove(dashboardPath); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "dashboard", "updated")
	saved, err = store.ListDashboards()
	if err != nil || len(saved) != 0 {
		t.Fatal("deleted dashboard remains")
	}
	alertPath := filepath.Join(alerts, "alert.yml")
	if err := os.WriteFile(alertPath, []byte("id: watched-alert\n"+alertDirectoryYAML), 0600); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "alert", "updated")
	rule, err := store.GetAlertRule("watched-alert")
	if err != nil || !rule.Updated {
		t.Fatalf("created alert: %v %v", rule, err)
	}
	if err := os.Remove(alertPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alerts); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "alert", "updated")
	if err := os.Mkdir(alerts, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alertPath, []byte("id: watched-alert\n"+alertDirectoryYAML), 0600); err != nil {
		t.Fatal(err)
	}
	waitDefinitionEvent(t, hub, "alert", "updated")
	if _, err := store.GetAlertRule("watched-alert"); err != nil {
		t.Fatal("recreated directory was not watched:", err)
	}
	stop() // Explicit close is safe even when called again by defer.
}

func TestDefinitionFingerprintIgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	before, err := definitionFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "editor.tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := definitionFingerprint(dir)
	if err != nil || before != after {
		t.Fatal("non-YAML file changed fingerprint")
	}
}
