package main

import (
	"fmt"
	"os"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func retentionConfig(days, maxSessions, maxDBSizeMB int) storage.RetentionConfig {
	cfg := storage.RetentionConfig{
		MaxSessions: maxSessions,
	}
	if days > 0 {
		cfg.MaxAge = time.Duration(days) * 24 * time.Hour
	}
	if maxDBSizeMB > 0 {
		cfg.MaxDBSizeBytes = int64(maxDBSizeMB) * 1024 * 1024
	}
	return cfg
}

// runRetention applies the retention policy immediately, then every hour for the
// lifetime of the process. Errors are logged but never fatal.
func runRetention(store *storage.DB, cfg storage.RetentionConfig, activeID string) {
	apply := func() {
		res, err := store.Prune(cfg, activeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "retention: %v\n", err)
			return
		}
		if res.DeletedByAge+res.DeletedByCount+res.DeletedBySize > 0 {
			fmt.Fprintf(os.Stderr,
				"retention: deleted %d sessions (age=%d count=%d size=%d) → %d remain, %.1f MB\n",
				res.DeletedByAge+res.DeletedByCount+res.DeletedBySize,
				res.DeletedByAge, res.DeletedByCount, res.DeletedBySize,
				res.FinalSessions, float64(res.FinalDBSizeBytes)/1024/1024,
			)
		}
	}
	apply()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		apply()
	}
}

// prune opens the DB and applies the retention policy once. The active session
// is unknown at CLI invocation time, so we pass "" — no session is special.
func prune(dbPath string, cfg storage.RetentionConfig) error {
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	res, err := store.Prune(cfg, "")
	if err != nil {
		return err
	}
	fmt.Printf("pruned: age=%d count=%d size=%d → %d sessions remain, %.1f MB on disk\n",
		res.DeletedByAge, res.DeletedByCount, res.DeletedBySize,
		res.FinalSessions, float64(res.FinalDBSizeBytes)/1024/1024,
	)
	return nil
}

// reset wipes all data in the DB file.
func reset(dbPath string) error {
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()
	if err := store.Reset(); err != nil {
		return err
	}
	fmt.Println("all spaniel data wiped")
	return nil
}
