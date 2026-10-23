package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zfogg/spaniel/internal/storage/querygen"
	"github.com/zfogg/spaniel/internal/telemetry"
	"gorm.io/gorm"
)

const (
	pruneTraceBatch     = 1000
	retentionRetryDelay = 5 * time.Minute
)

// ErrRetentionTargetUnreached means an optional reclamation target could not
// be reached safely. It does not mean the configured hard storage cap was hit.
var ErrRetentionTargetUnreached = errors.New("retention target not reached safely")

// Every joined branch must be the expected condition. A canceled statement
// joined with an appender reopen or I/O failure must remain a genuine error.
func onlyError(err, target error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyError(child, target) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyError(wrapped.Unwrap(), target)
	}
	return errors.Is(err, target)
}

func IsRetentionTargetUnreached(err error) bool { return onlyError(err, ErrRetentionTargetUnreached) }

// IsMaintenanceTimeout excludes mixed failures such as a deadline joined with
// an appender-restoration error, which must remain genuine storage failures.
func IsMaintenanceTimeout(err error) bool { return onlyError(err, context.DeadlineExceeded) }

// RetentionBackingOff reports whether eviction is temporarily deferred. WAL
// checkpoints must still run during this interval without deleting more data.
func (d *DB) RetentionBackingOff() bool {
	return time.Now().UnixNano() < d.retentionRetry.Load()
}

// RetentionConfig describes the retention policy applied by Prune.
// A zero value for any field disables that particular limit.
type RetentionConfig struct {
	MaxAge         time.Duration // delete sessions older than this
	MaxSessions    int           // keep at most this many sessions
	MaxDBSizeBytes int64         // shrink to at most this many bytes on disk
	MaxDuration    time.Duration // bound automatic maintenance; zero leaves explicit operator maintenance unbounded
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
func (d *DB) Prune(cfg RetentionConfig, activeID string) (res PruneResult, err error) {
	if cfg.MaxDuration > 0 && cfg.MaxDBSizeBytes > 0 && d.RetentionBackingOff() {
		return res, fmt.Errorf("%w: automatic reclamation is backing off", ErrRetentionTargetUnreached)
	}
	if cfg.MaxDuration > 0 {
		ctx, cancel := context.WithTimeout(d.gorm.Statement.Context, cfg.MaxDuration)
		defer cancel()
		d = d.WithContext(ctx)
	}
	started := time.Now()
	result := "ok"
	defer func() {
		if cfg.MaxDuration > 0 {
			if IsMaintenanceTimeout(err) {
				err = fmt.Errorf("%w: automatic maintenance budget exhausted", ErrRetentionTargetUnreached)
				d.retentionRetry.Store(time.Now().Add(30 * time.Second).UnixNano())
			} else if IsRetentionTargetUnreached(err) {
				d.retentionRetry.Store(time.Now().Add(retentionRetryDelay).UnixNano())
			}
		}
		if err != nil {
			result = "error"
		}
		telemetry.Catalog().RecordStoragePrune(context.Background(), result, float64(time.Since(started).Microseconds())/1000)
	}()
	// Automatic startup/hourly checks must not pause every appender merely to
	// discover that there is no age/count/size work. In particular, a below-cap
	// hourly pass must not queue a second rebuild behind the proactive guard.
	if cfg.MaxDuration > 0 && (cfg.MaxDBSizeBytes <= 0 || d.UsedSize() <= cfg.MaxDBSizeBytes) {
		needed := false
		if cfg.MaxSessions > 0 {
			var count int64
			count, err = d.query.Session.Count()
			if err != nil {
				return res, err
			}
			res.FinalSessions = int(count)
			needed = count > int64(cfg.MaxSessions)
		}
		if !needed && cfg.MaxAge > 0 {
			var ids []string
			ids, err = d.protectedSessionIDsOlderThan(time.Now().Add(-cfg.MaxAge).UnixNano(), activeID)
			if err != nil {
				return res, err
			}
			needed = len(ids) > 0
		}
		if !needed {
			res.FinalDBSizeBytes = d.FileSize()
			return res, nil
		}
	}

	err = d.withMaintenance(func(d *DB) error {
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
			n, err := d.deleteBySize(cfg.MaxDBSizeBytes, activeID, cfg.MaxDuration > 0)
			res.DeletedBySize = n
			if err != nil {
				return fmt.Errorf("delete by size: %w", err)
			}
		}
		if cfg.MaxDuration > 0 && cfg.MaxDBSizeBytes > 0 {
			return nil // the automatic size phase owns checkpoint scheduling
		}
		return d.checkpointWithRetry()
	})
	if err != nil {
		result = "error"
		return res, err
	}

	// Final state for reporting.
	var finalSessions int64
	count, err := d.query.Session.Count()
	if err != nil {
		result = "error"
		return res, err
	}
	finalSessions = count
	res.FinalSessions = int(finalSessions)
	res.FinalDBSizeBytes = d.FileSize()
	telemetry.Catalog().RecordRetention(context.Background(), "age", int64(res.DeletedByAge))
	telemetry.Catalog().RecordRetention(context.Background(), "count", int64(res.DeletedByCount))
	telemetry.Catalog().RecordRetention(context.Background(), "size", int64(res.DeletedBySize))
	return res, nil
}

// Reset wipes all telemetry data (spans, logs, lint warnings, trace issues, sessions).
// The active session pointer in memory is cleared.
func (d *DB) Reset() error {
	err := d.withMaintenance(func(d *DB) error {
		if err := d.resetTelemetry(); err != nil {
			return err
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
	if err := d.query.Session.
		Where(d.query.Session.ID.Neq(activeID), d.query.Session.IsBaseline.Is(false)).
		Order(d.query.Session.CreatedAt).
		Pluck(d.query.Session.ID, &deletable); err != nil {
		return 0, err
	}

	total, err := d.query.Session.Count()
	if err != nil {
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

func (d *DB) deleteBySize(maxBytes int64, activeID string, automatic ...bool) (int, error) {
	// A stale WAL or deleted indexed tuples must never trigger live-data
	// eviction. Reclaim first, then measure the remaining live allocation.
	bounded := len(automatic) > 0 && automatic[0]
	ready := false
	if bounded {
		ready = d.retentionCheckpointReady.Swap(false)
	}
	if bounded && (!ready || d.WALSize() >= ManagedWALCheckpointBytes) {
		// A checkpoint and a large rebuild/eviction cannot share one exporter
		// pause. Finish this phase, then resume ingestion before the next check.
		if err := d.checkpointWithRetry(); err != nil {
			return 0, err
		}
		d.retentionCheckpointReady.Store(true)
		return 0, nil
	}
	progress := reclaimProgress{OneTable: bounded, Visited: d.retentionRebuildVisited.Load()}
	var reclaimErr error
	if bounded {
		// Materialize the small WAL accumulated since the prior phase before
		// measuring allocation. Otherwise new writes can mask a successful
		// rebuild or WAL compression can masquerade as reclaimed indexes.
		if err := d.checkpointWithRetry(); err != nil {
			return 0, err
		}
		progress.BeforeRebuild = d.UsedSize()
		reclaimErr = d.rebuildTelemetry(&progress)
		d.retentionRebuildVisited.Store(progress.Visited)
	} else {
		reclaimErr = d.reclaimRetentionSpace(&progress)
	}
	if reclaimErr != nil {
		return 0, reclaimErr
	}
	if !bounded && progress.HadTelemetryDeletes && progress.Rebuilt && d.UsedSize() >= progress.BeforeRebuild && d.UsedSize() > maxBytes {
		d.retentionFloor.Store(d.UsedSize())
		return 0, fmt.Errorf("%w: existing deleted telemetry reclaimed no allocation; refusing live eviction", ErrRetentionTargetUnreached)
	}
	if bounded && (progress.Rebuilt || d.retentionReclaimed.Load()) {
		// Compare around the actual rebuild, after the checkpoint materializes
		// incoming WAL. Ingestion between passes must not mask reclaimed blocks,
		// and WAL compression alone must not indefinitely postpone eviction.
		after := d.UsedSize()
		if after < progress.BeforeRebuild {
			d.retentionReclaimed.Store(true)
		}
		if progress.Remaining {
			// A small first table cannot establish a storage floor while larger
			// affected tables still contain deleted tuples. Finish their bounded
			// phases before judging the entire eviction/reclamation cycle.
			d.retentionCheckpointReady.Store(true)
			return 0, nil
		}
		// The floor was sampled after a checkpoint under this same exclusive
		// pause, before deletion. It also proves progress if a prior committed
		// copy timed out during its final checkpoint and was measured next pass.
		if d.retentionReclaimed.Swap(false) || (d.retentionFloor.Load() > 0 && after < d.retentionFloor.Load()) {
			d.retentionFloor.Store(0)
		} else if d.retentionFloor.Load() > 0 {
			d.retentionFloor.Store(after)
			return 0, fmt.Errorf("%w: retention reclaimed no storage; refusing further telemetry deletion", ErrRetentionTargetUnreached)
		}
		return 0, nil
	}
	if floor := d.retentionFloor.Load(); floor > maxBytes && d.UsedSize() >= floor {
		return 0, fmt.Errorf("%w: prior deletion made no allocation progress; refusing further telemetry deletion", ErrRetentionTargetUnreached)
	}
	deleted := 0
	for d.UsedSize() > maxBytes {
		before := d.UsedSize()
		var n int
		var err error
		if bounded {
			n, err = d.deleteOldestTelemetryBatch(activeID, float64(before-maxBytes)/float64(before))
		} else {
			n, err = d.deleteOldestTelemetryBatch(activeID)
		}
		if err != nil {
			return deleted, err
		}
		if n == 0 {
			break
		}
		deleted += n
		// Protect against another deletion if this reclamation is interrupted.
		// Only a measured allocation decrease permits another oldest batch.
		d.retentionFloor.Store(before)
		d.retentionReclaimed.Store(false)
		d.retentionRebuildVisited.Store(0)
		if bounded {
			// Evict one coherent oldest prefix, then let the next guard pass
			// reclaim it. Never chain several rebuilds in one ingestion pause.
			return deleted, nil
		}
		if err := d.reclaimRetentionSpace(); err != nil {
			return deleted, err
		}
		if d.UsedSize() >= before {
			d.retentionFloor.Store(before)
			return deleted, fmt.Errorf("%w: retention reclaimed no storage; refusing further telemetry deletion", ErrRetentionTargetUnreached)
		}
	}
	d.retentionFloor.Store(0)
	if bounded {
		d.retentionRebuildVisited.Store(0)
	}
	return deleted, nil
}

// reclaimRetentionSpace makes the size measurement authoritative before and
// after deletion, including indexed row groups that CHECKPOINT cannot compact.
type reclaimProgress struct {
	Rebuilt             bool
	BeforeRebuild       int64
	OneTable            bool
	Remaining           bool
	Visited             uint64
	HadTelemetryDeletes bool
}

func (d *DB) reclaimRetentionSpace(progress ...*reclaimProgress) error {
	if err := d.checkpointWithRetry(); err != nil {
		return err
	}
	if len(progress) > 0 {
		progress[0].BeforeRebuild = d.UsedSize()
	}
	return d.rebuildTelemetry(progress...)
}

// deleteOldestTelemetryBatch selects coherent trace groups and independent logs
// and metrics in global age order. Auxiliary trace rows leave before their spans.
// It returns the number of primary telemetry rows removed.
func (d *DB) deleteOldestTelemetryBatch(activeID string, sizeFraction ...float64) (int, error) {
	// Use one age-ordered queue across all signals. Small proportional batches
	// avoid discarding 1,000 traces (or all logs) to free a few database blocks.
	// Correlated logs leave with their trace rather than being pruned early.
	candidates := `
		SELECT 'trace' AS kind, session_id, trace_id, 0::BIGINT AS record_id, MIN(start_ns) AS age
		FROM spans GROUP BY session_id, trace_id
		UNION ALL SELECT 'log', session_id, '', rowid, timestamp_ns FROM logs l
		WHERE NOT EXISTS (SELECT 1 FROM spans s WHERE s.session_id = l.session_id AND s.trace_id = l.trace_id)
		UNION ALL SELECT 'metric', session_id, '', rowid, timestamp_ns FROM metrics`
	var groups int64
	if err := d.namedGORM("RetentionCandidateCount").Raw(`SELECT COUNT(*) FROM (` + candidates + `) candidates`).Scan(&groups).Error; err != nil {
		return 0, err
	}
	batch := min(int64(pruneTraceBatch), max(int64(1), (groups+99)/100))
	if len(sizeFraction) > 0 && sizeFraction[0] < 0 {
		// A physical rewrite copies every surviving row, not just this prefix.
		// A fixed 32K limit made large stores repeat that whole-file copy many
		// times. Scale the coherent oldest prefix to the measured size deficit,
		// retaining at least half the groups and a bounded transaction size.
		fraction := min(0.5, max(0.1, -sizeFraction[0]))
		batch = min(int64(1<<20), max(int64(1), int64(float64(groups)*fraction)))
	}
	// Large stores need a physically meaningful oldest prefix. Tiny stores
	// retain conservative batches and the protection against metadata floors.
	if len(sizeFraction) > 0 && sizeFraction[0] >= 0 && groups >= 262144 {
		batch = min(int64(131072), max(batch, min(groups/10, int64(float64(groups)*sizeFraction[0]))))
	}
	if err := d.namedGORM("RetentionTraceBatchCreate").Exec(fmt.Sprintf(`
		CREATE OR REPLACE TEMP TABLE retention_trace_batch AS
		SELECT * FROM (%s) candidates ORDER BY age, kind, session_id, trace_id, record_id LIMIT %d`, candidates, batch)).Error; err != nil {
		return 0, fmt.Errorf("materialize retention batch: %w", err)
	}
	defer func() {
		_ = d.namedGORM("RetentionTraceBatchDrop").Exec("DROP TABLE IF EXISTS retention_trace_batch").Error
	}()
	oldestTraces := "SELECT session_id, trace_id FROM retention_trace_batch WHERE kind = 'trace'"
	traceMatch := func(table string) string {
		return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM (%s) oldest
			WHERE oldest.session_id = %s.session_id
			  AND oldest.trace_id = %s.trace_id
		)`, oldestTraces, table, table)
	}

	deleted := 0
	err := d.maintenanceTransaction(d.namedGORM("DeleteOldestTelemetryBatch"), func(tx *gorm.DB) error {
		for _, table := range []string{"lint_warnings", "trace_issues", "logs", "span_events", "span_links"} {
			if err := tx.Exec(`DELETE FROM ` + table + ` WHERE ` + traceMatch(table)).Error; err != nil {
				return fmt.Errorf("delete old trace %s: %w", table, err)
			}
		}
		result := tx.Exec(`DELETE FROM spans WHERE ` + traceMatch("spans"))
		if result.Error != nil {
			return fmt.Errorf("delete old spans: %w", result.Error)
		}
		deleted += int(result.RowsAffected)
		for _, spec := range []struct{ table, kind string }{{"logs", "log"}, {"metrics", "metric"}} {
			result = tx.Exec(`DELETE FROM `+spec.table+` WHERE rowid IN (
				SELECT record_id FROM retention_trace_batch WHERE kind = ?
			)`, spec.kind)
			if result.Error != nil {
				return fmt.Errorf("delete old %s: %w", spec.table, result.Error)
			}
			deleted += int(result.RowsAffected)
		}
		// Sessions are metadata, not a reason to retain an empty allocation.
		// Preserve the active and baseline records so new telemetry continues to
		// land in a valid session.
		if err := tx.Exec(`
		DELETE FROM sessions
		WHERE id != ? AND is_baseline = FALSE
		  AND NOT EXISTS (SELECT 1 FROM spans WHERE spans.session_id = sessions.id)
		  AND NOT EXISTS (SELECT 1 FROM logs WHERE logs.session_id = sessions.id)
		  AND NOT EXISTS (SELECT 1 FROM metrics WHERE metrics.session_id = sessions.id)
		`, activeID).Error; err != nil {
			return fmt.Errorf("delete empty sessions: %w", err)
		}
		return nil
	})
	return deleted, err
}

func (d *DB) protectedSessionIDsOlderThan(cutoffNs int64, activeID string) ([]string, error) {
	var ids []string
	err := d.query.Session.
		Where(d.query.Session.CreatedAt.Lt(cutoffNs), d.query.Session.ID.Neq(activeID), d.query.Session.IsBaseline.Is(false)).
		Pluck(d.query.Session.ID, &ids)
	return ids, err
}

func (d *DB) resetTelemetry() error {
	// Generated DAOs retain model ownership and the normal instrumentation path;
	// each predicate makes the intended whole-table maintenance deletion explicit.
	return d.maintenanceTransaction(d.namedGORM("ResetTelemetry"), func(g *gorm.DB) error {
		tx := querygen.Use(g)
		if _, err := tx.LintWarning.Where(tx.LintWarning.SpanID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate lint_warnings: %w", err)
		}
		if _, err := tx.TraceIssue.Where(tx.TraceIssue.ID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate trace_issues: %w", err)
		}
		if _, err := tx.Log.Where(tx.Log.TimestampNs.Gte(0)).Delete(); err != nil {
			return fmt.Errorf("truncate logs: %w", err)
		}
		if _, err := tx.Metric.Where(tx.Metric.TimestampNs.Gte(0)).Delete(); err != nil {
			return fmt.Errorf("truncate metrics: %w", err)
		}
		if _, err := tx.SpanEvent.Where(tx.SpanEvent.SpanID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate span_events: %w", err)
		}
		if _, err := tx.SpanLink.Where(tx.SpanLink.SpanID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate span_links: %w", err)
		}
		if _, err := tx.Span.Where(tx.Span.SpanID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate spans: %w", err)
		}
		if _, err := tx.Session.Where(tx.Session.ID.IsNotNull()).Delete(); err != nil {
			return fmt.Errorf("truncate sessions: %w", err)
		}
		return nil
	})
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
