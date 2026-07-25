package storage

import (
	"fmt"
	"time"
)

const (
	pruneTraceBatch  = 1000
	pruneRecordBatch = 10000
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

// Prune applies the retention policy. Age and count retention preserve the
// active and baseline sessions. The hard size limit instead evicts the oldest
// telemetry inside every session, including the active session, because a
// continuously active session must not be allowed to grow past the cap.
func (d *DB) Prune(cfg RetentionConfig, activeID string) (PruneResult, error) {
	var res PruneResult

	err := d.withMaintenance(func() error {
		if cfg.MaxAge > 0 {
			n, err := d.deleteByAge(cfg.MaxAge, activeID)
			if err != nil {
				return fmt.Errorf("delete by age: %w", err)
			}
			res.DeletedByAge = n
		}

		if cfg.MaxSessions > 0 {
			n, err := d.deleteByCount(cfg.MaxSessions, activeID)
			if err != nil {
				return fmt.Errorf("delete by count: %w", err)
			}
			res.DeletedByCount = n
		}

		if cfg.MaxDBSizeBytes > 0 && d.path != "" && d.path != ":memory:" {
			n, err := d.deleteBySize(cfg.MaxDBSizeBytes, activeID)
			if err != nil {
				return fmt.Errorf("delete by size: %w", err)
			}
			res.DeletedBySize = n
		}
		return d.checkpointWithRetry()
	})
	if err != nil {
		return res, err
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
	err := d.withMaintenance(func() error {
		for _, tbl := range []string{"lint_warnings", "trace_issues", "logs", "metrics", "span_events", "span_links", "spans", "sessions"} {
			if err := d.gorm.Exec(`DELETE FROM ` + tbl).Error; err != nil {
				return fmt.Errorf("truncate %s: %w", tbl, err)
			}
		}
		return d.checkpointWithRetry()
	})
	if err != nil {
		return err
	}
	d.activeSessionID = ""
	d.activeSessionLabel = ""
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
	deleted := 0
	for d.UsedSize() > maxBytes {
		n, err := d.deleteOldestTelemetryBatch(activeID)
		if err != nil {
			return deleted, err
		}
		if n == 0 {
			break
		}
		deleted += n
		if err := d.checkpointWithRetry(); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// deleteOldestTelemetryBatch removes coherent oldest trace groups first, then
// independently aged logs and metrics. Auxiliary trace rows are deleted before
// their spans. It returns the number of primary telemetry rows removed.
func (d *DB) deleteOldestTelemetryBatch(activeID string) (int, error) {
	oldestTraces := fmt.Sprintf(`
		SELECT session_id, trace_id
		FROM spans
		GROUP BY session_id, trace_id
		ORDER BY MIN(start_ns), session_id, trace_id
		LIMIT %d`, pruneTraceBatch)
	traceMatch := func(table string) string {
		return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM (%s) oldest
			WHERE oldest.session_id = %s.session_id
			  AND oldest.trace_id = %s.trace_id
		)`, oldestTraces, table, table)
	}

	for _, table := range []string{"lint_warnings", "trace_issues", "logs", "span_events", "span_links"} {
		if err := d.gorm.Exec(`DELETE FROM ` + table + ` WHERE ` + traceMatch(table)).Error; err != nil {
			return 0, fmt.Errorf("delete old trace %s: %w", table, err)
		}
	}

	deleted := 0
	tx := d.gorm.Exec(`DELETE FROM spans WHERE ` + traceMatch("spans"))
	if tx.Error != nil {
		return 0, fmt.Errorf("delete old spans: %w", tx.Error)
	}
	deleted += int(tx.RowsAffected)

	for _, spec := range []struct {
		table string
		order string
	}{
		{"logs", "timestamp_ns"},
		{"metrics", "timestamp_ns"},
	} {
		q := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (
			SELECT rowid FROM %s ORDER BY %s LIMIT %d
		)`, spec.table, spec.table, spec.order, pruneRecordBatch)
		tx = d.gorm.Exec(q)
		if tx.Error != nil {
			return deleted, fmt.Errorf("delete old %s: %w", spec.table, tx.Error)
		}
		deleted += int(tx.RowsAffected)
	}

	// Sessions are metadata, not a reason to retain an empty allocation.
	// Preserve the active and baseline records so new telemetry continues to
	// land in a valid session.
	if err := d.gorm.Exec(`
		DELETE FROM sessions
		WHERE id != ? AND is_baseline = FALSE
		  AND NOT EXISTS (SELECT 1 FROM spans WHERE spans.session_id = sessions.id)
		  AND NOT EXISTS (SELECT 1 FROM logs WHERE logs.session_id = sessions.id)
		  AND NOT EXISTS (SELECT 1 FROM metrics WHERE metrics.session_id = sessions.id)
	`, activeID).Error; err != nil {
		return deleted, fmt.Errorf("delete empty sessions: %w", err)
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

// checkpoint is retained for internal callers and tests that need an immediate
// durable size measurement. Unlike the old best-effort helper, it reports
// maintenance failures to callers that choose to inspect them.
func (d *DB) checkpoint() error {
	return d.flushBeforeCheckpoint()
}
