package storage

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"time"
)

type SignalRetention struct{ TracesDays, LogsDays, MetricsDays int }

// PruneSignals expires telemetry independently, including the active session,
// while preserving baselines and deleting complete traces and their metadata.
func (d *DB) PruneSignals(policy SignalRetention) error {
	if policy.TracesDays <= 0 && policy.LogsDays <= 0 && policy.MetricsDays <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	d = d.WithContext(ctx)
	return d.withMaintenance(func(d *DB) error {
		return d.maintenanceTransaction(d.namedGORM("SignalRetention"), func(tx *gorm.DB) error {
			if policy.TracesDays > 0 {
				cutoff := time.Now().Add(-time.Duration(policy.TracesDays) * 24 * time.Hour).UnixNano()
				if e := tx.Exec(`CREATE OR REPLACE TEMP TABLE signal_retention_traces AS SELECT session_id, trace_id FROM spans WHERE session_id NOT IN (SELECT id FROM sessions WHERE is_baseline) GROUP BY session_id, trace_id HAVING MAX(start_ns + duration_ns) < ?`, cutoff).Error; e != nil {
					return e
				}
				defer tx.Exec("DROP TABLE IF EXISTS signal_retention_traces")
				for _, table := range []string{"lint_warnings", "trace_issues", "span_events", "span_links", "spans"} {
					if e := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE EXISTS (SELECT 1 FROM signal_retention_traces r WHERE r.session_id=%s.session_id AND r.trace_id=%s.trace_id)`, table, table, table)).Error; e != nil {
						return e
					}
				}
			}
			for _, s := range []struct {
				table string
				days  int
			}{{"logs", policy.LogsDays}, {"metrics", policy.MetricsDays}} {
				if s.days > 0 {
					cutoff := time.Now().Add(-time.Duration(s.days) * 24 * time.Hour).UnixNano()
					if e := tx.Exec("DELETE FROM "+s.table+" WHERE timestamp_ns < ? AND session_id NOT IN (SELECT id FROM sessions WHERE is_baseline)", cutoff).Error; e != nil {
						return e
					}
				}
			}
			return nil
		})
	})
}
