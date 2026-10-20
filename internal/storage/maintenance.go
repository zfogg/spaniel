package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"github.com/zfogg/spaniel/internal/telemetry"
	"gorm.io/gorm"
)

// Checkpoint flushes pending appender work and makes a subsequent storage-size
// query observe the committed database state.
func (d *DB) Checkpoint() error { return d.flushBeforeCheckpoint() }

// StorageBreakdown gives a per-table and per-session storage summary for the
// Settings UI and the `spaniel compact` command.
type StorageBreakdown struct {
	Tables           []TableStat   `json:"tables"`
	Sessions         []SessionSize `json:"sessions"` // top 10 by approx bytes
	WALBytes         int64         `json:"wal_bytes"`
	MainBytes        int64         `json:"main_bytes"`
	LastCheckpointAt int64         `json:"last_checkpoint_at"`
}

type TableStat struct {
	Name        string `json:"name"`
	RowCount    int64  `json:"row_count"`
	ApproxBytes int64  `json:"approx_bytes"`
}

type SessionSize = model.SessionSize

// GetStorageBreakdown returns per-table sizes via duckdb_tables() and a
// complete, session-scoped telemetry payload estimate. DuckDB does not expose
// ownership for compressed blocks shared by multiple sessions.
func (d *DB) GetStorageBreakdown() (*StorageBreakdown, error) {
	out := &StorageBreakdown{}

	// Table sizes are live payload estimates. Physical allocation, indexes and
	// deleted tuples are reported separately in MainBytes/WALBytes.
	if d.path != "" && d.path != ":memory:" {
		_ = d.flushBeforeCheckpoint()
	}
	var fileBytes int64
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			fileBytes = fi.Size()
		}
	}

	tableNames := []string{"spans", "logs", "metrics", "span_events", "span_links", "sessions", "trace_issues", "lint_warnings"}
	sizes, _ := d.query.Span.StorageTableSizes()

	byTable := make(map[string]model.TableSizeRow, len(sizes))
	for _, s := range sizes {
		byTable[s.Name] = s
	}
	for _, name := range tableNames {
		s := byTable[name]
		approxBytes := s.PayloadBytes
		out.Tables = append(out.Tables, TableStat{
			Name:        name,
			RowCount:    s.RowCount,
			ApproxBytes: approxBytes,
		})
	}

	// Per-session: include every session-scoped telemetry table. Their compact
	// payloads are the deterministic basis for allocating shared DuckDB blocks.
	sessSizes, _ := d.query.Span.TopSessionSizes()
	out.Sessions = sessSizes

	// File sizes: MainBytes comes from fileBytes computed above (post-checkpoint).
	out.MainBytes = fileBytes
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path + ".wal"); err == nil {
			out.WALBytes = fi.Size()
		}
	}

	return out, nil
}

// CompactResult reports physical bytes before and after reclaiming deleted rows.
type CompactResult struct {
	BytesBefore int64 `json:"bytes_before"`
	BytesAfter  int64 `json:"bytes_after"`
	Reclaimed   int64 `json:"reclaimed"`
}

// Compact rebuilds tables containing deleted tuples, preserving live rows,
// constraints and indexes. DuckDB may retain free blocks for future writes.
func (d *DB) Compact() (*CompactResult, error) {
	res := &CompactResult{}
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			res.BytesBefore = fi.Size()
		}
	}
	if err := d.withMaintenance(func(d *DB) error {
		if err := d.checkpointWithRetry(); err != nil {
			return fmt.Errorf("checkpoint: %w", err)
		}
		return d.rebuildTelemetry()
	}); err != nil {
		return res, err
	}
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			res.BytesAfter = fi.Size()
		}
	}
	if res.BytesBefore > res.BytesAfter {
		res.Reclaimed = res.BytesBefore - res.BytesAfter
	}
	return res, nil
}

// flushBeforeCheckpoint flushes the batcher's pending rows and then runs
// CHECKPOINT. This prevents DuckDB internal state corruption that occurs when
// CHECKPOINT interacts with unflushed appender data in the CGo layer.
func (d *DB) flushBeforeCheckpoint() error {
	return d.withMaintenance(func(d *DB) error {
		return d.checkpointWithRetry()
	})
}

func (d *DB) withMaintenance(fn func(*DB) error) error {
	telemetry.Catalog().SetStorageMaintenanceInFlight(1)
	defer telemetry.Catalog().SetStorageMaintenanceInFlight(0)
	if d.batcher == nil {
		return d.exclusiveSQLMaintenance(fn)
	}
	return d.batcher.Maintenance(func() error { return d.exclusiveSQLMaintenance(fn) })
}

// Pause appenders and drain readers/metadata transactions before DuckDB
// maintenance. A checkpoint must not race native prepares on another connection.
// This restores the isolation already proven in the collector incident repair.
func (d *DB) exclusiveSQLMaintenance(fn func(*DB) error) error {
	ctx, cancel := context.WithTimeout(d.gorm.Statement.Context, 30*time.Second)
	defer cancel()
	pool := d.SQL()
	limit := pool.Stats().MaxOpenConnections
	if limit <= 0 {
		return fmt.Errorf("maintenance requires a bounded SQL pool")
	}
	var conns []*sql.Conn
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for range limit {
		conn, err := pool.Conn(ctx)
		if err != nil {
			return fmt.Errorf("drain SQL pool for maintenance: %w", err)
		}
		conns = append(conns, conn)
	}
	maintenance := *d
	// The drain deadline bounds acquisition, not the maintenance transaction.
	// Retention may need longer than 30s on an existing large file; interrupting
	// it midway repeatedly prevents the storage guard from restoring headroom.
	maintenance.gorm = d.gorm.Session(&gorm.Session{NewDB: true, Context: d.gorm.Statement.Context})
	maintenance.gorm.Statement.ConnPool = conns[0]
	maintenance.query = querygen.Use(maintenance.gorm)
	return fn(&maintenance)
}

// checkpointWithRetry lets auxiliary GORM writes from requests accepted just
// before maintenance finish. Appenders remain paused by withMaintenance, so
// the set of outstanding writers only drains; a transient writer collision
// must not make the storage guard declare the database full.
func (d *DB) checkpointWithRetry() error {
	started := time.Now()
	defer func() {
		telemetry.Catalog().RecordStorageCheckpoint(context.Background(), float64(time.Since(started).Microseconds())/1000)
	}()
	var err error
	for range 80 {
		err = d.gorm.Exec("CHECKPOINT").Error
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "other write transactions active") {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}
