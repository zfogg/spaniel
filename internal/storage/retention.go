package storage

import (
	"fmt"
	"os"
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
	DeletedByAge     int
	DeletedByCount   int
	DeletedBySize    int
	FinalSessions    int
	FinalDBSizeBytes int64
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
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&res.FinalSessions); err != nil {
		return res, err
	}
	res.FinalDBSizeBytes = d.fileSize()
	return res, nil
}

// Reset wipes all telemetry data (spans, logs, lint warnings, trace issues, sessions).
// The active session pointer in memory is cleared.
func (d *DB) Reset() error {
	for _, tbl := range []string{"lint_warnings", "trace_issues", "logs", "metrics", "spans", "sessions"} {
		if _, err := d.db.Exec(`DELETE FROM ` + tbl); err != nil {
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
	rows, err := d.db.Query(`
		SELECT id FROM sessions
		WHERE id != ? AND is_baseline = FALSE
		ORDER BY created_at ASC`, activeID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var deletable []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		deletable = append(deletable, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var total int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&total); err != nil {
		return 0, err
	}
	excess := total - maxSessions
	if excess <= 0 {
		return 0, nil
	}
	if excess > len(deletable) {
		excess = len(deletable)
	}
	return d.deleteSessions(deletable[:excess])
}

func (d *DB) deleteBySize(maxBytes int64, activeID string) (int, error) {
	size := d.fileSize()
	if size <= maxBytes {
		return 0, nil
	}

	rows, err := d.db.Query(`
		SELECT id FROM sessions
		WHERE id != ? AND is_baseline = FALSE
		ORDER BY created_at ASC`, activeID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		candidates = append(candidates, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	deleted := 0
	for _, id := range candidates {
		if d.fileSize() <= maxBytes {
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
	rows, err := d.db.Query(`
		SELECT id FROM sessions
		WHERE created_at < ? AND id != ? AND is_baseline = FALSE`, cutoffNs, activeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) deleteSessions(ids []string) (int, error) {
	for _, id := range ids {
		if err := d.DeleteSession(id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (d *DB) fileSize() int64 {
	if d.path == "" || d.path == ":memory:" {
		return 0
	}
	fi, err := os.Stat(d.path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// checkpoint flushes WAL into the main DB file so file-size measurements are
// meaningful immediately after a delete. Best-effort; errors are ignored.
func (d *DB) checkpoint() {
	_, _ = d.db.Exec(`CHECKPOINT`)
}
