package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// Also serializes the manual Settings reload with filesystem reconciliation.
var definitionReloadMu sync.Mutex

type definitionDirectory struct {
	kind, path  string
	load        func(*storage.DB, string) error
	fingerprint string
}

func definitionFingerprint(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, entry := range entries {
		if entry.IsDir() || !definitionYAML(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s:%x\n", entry.Name(), sha256.Sum256(data))
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func definitionYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

// fsnotify uses ReadDirectoryChangesW on Windows, kqueue on macOS and inotify
// on Linux. Watch directories rather than files to support atomic editor saves.
// Parent watches allow a deleted/replaced directory to be attached again.
func watchDefinitionDirectories(ctx context.Context, store *storage.DB, hub uiBroadcaster, dashboards, alerts string) (func(), error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	var dirs []*definitionDirectory
	for _, d := range []*definitionDirectory{
		{kind: "dashboard", path: dashboards, load: loadDashboardDefinitions},
		{kind: "alert", path: alerts, load: loadAlertDefinitions},
	} {
		if d.path == "" {
			continue
		}
		d.path, err = filepath.Abs(d.path)
		if err == nil {
			err = os.MkdirAll(d.path, 0700)
		}
		if err != nil {
			watcher.Close()
			return nil, err
		}
		for _, path := range []string{filepath.Dir(d.path), d.path} {
			if err := watcher.Add(path); err != nil {
				watcher.Close()
				return nil, err
			}
		}
		dirs = append(dirs, d)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	emit := func(d *definitionDirectory, status, message string) {
		if hub != nil {
			hub.Broadcast(&ws.Event{Type: "definition_sync", Timestamp: time.Now().UnixNano(), Payload: map[string]string{"kind": d.kind, "status": status, "message": message}})
		}
	}
	reconcile := func(d *definitionDirectory) {
		fingerprint, err := definitionFingerprint(d.path)
		if err == nil && fingerprint == d.fingerprint {
			return
		}
		emit(d, "updating", "")
		if err == nil {
			err = d.load(store, d.path)
		}
		if err != nil {
			slog.Error("reload YAML definitions", "kind", d.kind, "directory", d.path, "error", err)
			emit(d, "error", err.Error())
			return
		}
		// Include IDs written by the loader, avoiding a second self-triggered reload.
		d.fingerprint, _ = definitionFingerprint(d.path)
		emit(d, "updated", "")
	}
	go func() {
		defer close(done)
		defer watcher.Close()
		timer := time.NewTimer(0) // Close the startup load/watch registration gap.
		defer timer.Stop()
		pending := make(map[*definitionDirectory]bool)
		for _, d := range dirs {
			pending[d] = true
		}
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				for _, d := range dirs {
					name := filepath.Clean(event.Name)
					if strings.EqualFold(name, d.path) || (strings.EqualFold(filepath.Dir(name), d.path) && definitionYAML(name)) {
						// Add is idempotent and reattaches a recreated directory.
						if info, err := os.Stat(d.path); err == nil && info.IsDir() {
							_ = watcher.Add(d.path)
						}
						pending[d] = true
					}
				}
				if len(pending) > 0 {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(150 * time.Millisecond)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("watch YAML definitions", "error", err)
				for _, d := range dirs {
					pending[d] = true
				}
				timer.Reset(150 * time.Millisecond)
			case <-timer.C:
				for _, d := range dirs {
					if pending[d] {
						reconcile(d)
						delete(pending, d)
					}
				}
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}
