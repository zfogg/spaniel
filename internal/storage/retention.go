package storage

import (
	"fmt"
	"time"
)

// RetentionConfig describes the retention policy applied by Prune.
// A zero value for any field disables that particular limit.
type RetentionConfig struct {
	MaxAge         time.Duration // delete sessions older than this
	MaxSessions    int           // keep at most this many sessions
	MaxDBSizeBytes int64         // shrink to at most this many bytes on disk
}

// PruneResult summarizes what a Prune run did. Useful for logging and tests.
type PruneResult struct {
	DeletedByAge     int   `json:"deleted_by_age"`
	DeletedByCount   int   `json:"deleted_by_count"`
	DeletedBySize    int   `json:"deleted_by_size"`
	FinalSessions    int   `json:"final_sessions"`
	FinalDBSizeBytes int64 `json:"final_db_size_bytes"`
}

// Prune applies the retention policy. The activeID and any baseline sessions are
// never deleted, regardless of age or count.
func (d *DB) Prune(cfg RetentionConfig, activeID string) (PruneResult, error) {
	var res PruneResult

	if cfg.MaxAge > 0 {
		n, err := d.deleteByAge(cfg.MaxAge, activeID)
		if err != nil {
			return res, fmt.Errorf("delete by age: %w", err)
		}
		res.DeletedByAge = n
	}

	if cfg.MaxSessions > 0 {
		n, err := d.deleteByCount(cfg.MaxSessions, activeID)
		if err != nil {
			return res, fmt.Errorf("delete by count: %w", err)
		}
		res.DeletedByCount = n
	}

	if cfg.MaxDBSizeBytes > 0 && d.path != "" && d.path != ":memory:" {
		n, err := d.deleteBySize(cfg.MaxDBSizeBytes, activeID)
		if err != nil {
			return res, fmt.Errorf("delete by size: %w", err)
		}
		res.DeletedBySize = n
	}

	// Final state for reporting.
	var finalSessions int64
	if err := d.gorm.Table("sessions").Count(&finalSessions).Error; err != nil {
		return res, err
	}
	res.FinalSessions = int(finalSessions)
	res.FinalDBSizeBytes = d.FileSize()
	return res, nil
}

// Reset wipes all telemetry data (spans, logs, lint warnings, trace issues, sessions).
// The active session pointer in memory is cleared.
func (d *DB) Reset() error {
	for _, tbl := range []string{"lint_warnings", "trace_issues", "logs", "metrics", "span_events", "span_links", "spans", "sessions"} {
		if err := d.gorm.Exec(`DELETE FROM ` + tbl).Error; err != nil {
			return fmt.Errorf("truncate %s: %w", tbl, err)
		}
	}
	d.activeSessionID = ""
	d.activeSessionLabel = ""
	d.checkpoint()
	return nil
}

// Path returns the underlying DuckDB file path (":memory:" for in-memory DBs).
func (d *DB) Path() string { return d.path }

// ── internals ────────────────────────────────────────────────────────────────

func (d *DB) deleteByAge(maxAge time.Duration, activeID string) (int, error) {
	cutoff := time.Now().Add(-maxAge).UnixNano()
	ids, err := d.protectedSessionIDsOlderThan(cutoff, activeID)
	if err != nil {
		return 0, err
	}
	return d.deleteSessions(ids)
}

func (d *DB) deleteByCount(maxSessions int, activeID string) (int, error) {
	// Oldest deletable sessions first; preserve active + baseline.
	var deletable []string
	if err := d.gorm.Table("sessions").
		Where("id != ? AND is_baseline = FALSE", activeID).
		Order("created_at ASC").
		Pluck("id", &deletable).Error; err != nil {
		return 0, err
	}

	var total int64
	if err := d.gorm.Table("sessions").Count(&total).Error; err != nil {
		return 0, err
	}
	excess := int(total) - maxSessions
	if excess <= 0 {
		return 0, nil
	}
	if excess > len(deletable) {
		excess = len(deletable)
	}
	return d.deleteSessions(deletable[:excess])
}

func (d *DB) deleteBySize(maxBytes int64, activeID string) (int, error) {
	size := d.FileSize()
	if size <= maxBytes {
		return 0, nil
	}

	var candidates []string
	if err := d.gorm.Table("sessions").
		Where("id != ? AND is_baseline = FALSE", activeID).
		Order("created_at ASC").
		Pluck("id", &candidates).Error; err != nil {
		return 0, err
	}

	deleted := 0
	for _, id := range candidates {
		if d.FileSize() <= maxBytes {
			break
		}
		if _, err := d.deleteSessions([]string{id}); err != nil {
			return deleted, err
		}
		d.checkpoint()
		deleted++
	}
	return deleted, nil
}

func (d *DB) protectedSessionIDsOlderThan(cutoffNs int64, activeID string) ([]string, error) {
	var ids []string
	err := d.gorm.Table("sessions").
		Where("created_at < ? AND id != ? AND is_baseline = FALSE", cutoffNs, activeID).
		Pluck("id", &ids).Error
	return ids, err
}

func (d *DB) deleteSessions(ids []string) (int, error) {
	for _, id := range ids {
		if err := d.DeleteSession(id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// checkpoint flushes the batcher and WAL into the main DB file so file-size
// measurements are meaningful immediately after a delete. Best-effort; errors
// are ignored.
func (d *DB) checkpoint() {
	_ = d.flushBeforeCheckpoint()
}
