package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/alifiroozi80/duckdb"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"github.com/zfogg/spaniel/internal/telemetry"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	pool                     *switchablePool
	maintenanceMu            *sync.Mutex
	gorm                     *gorm.DB
	query                    *querygen.Query
	batcher                  *Batcher
	path                     string
	activeSessionID          string
	activeSessionLabel       string
	full                     *atomic.Bool   // writes rejected when true (see full.go); shared across WithContext copies
	traceLintMu              *sync.Mutex    // shared by WithContext copies; serializes incremental trace lint insertion
	retentionFloor           *atomic.Int64  // failed reclamation floor; prevents repeated destructive retries
	retentionRetry           *atomic.Int64  // automatic size reclamation retry time, Unix nanoseconds
	retentionCheckpointReady *atomic.Bool   // next automatic size pass may rebuild or evict after a separate checkpoint pass
	retentionReclaimed       *atomic.Bool   // measured allocation progress across one-table rebuild phases
	retentionRebuildVisited  *atomic.Uint64 // tables already reclaimed in the current oldest-prefix cycle
	retentionPhysicalReady   *atomic.Bool   // fragmentation was reclaimed; next automatic phase may evict
	retentionPhysicalFreed   *atomic.Int64  // measured bytes reclaimed by the previous physical eviction
	retentionPhysicalRatio   *atomic.Uint64 // float64 bits: fraction of oldest groups selected in that eviction
	maintenanceOwned         bool           // all SQL leases are reserved until maintenance and recovery finish
}

// WithContext returns a shallow copy of DB whose GORM queries carry ctx, so the
// OTel plugin (see otel.go) nests DuckDB query spans under the caller's span.
// Use it on request paths: store.WithContext(req.Context()).ListTraces(...).
// Batcher (Appender) writes bypass GORM and are unaffected; instrument those
// with an explicit span at the call site.
func (d *DB) WithContext(ctx context.Context) *DB {
	cp := *d
	cp.gorm = d.gorm.WithContext(ctx)
	cp.query = querygen.Use(cp.gorm)
	return &cp
}

// namedQuery preserves the caller context while attaching a stable,
// source-owned span name to generated SQL whose implementation executes via
// GORM's raw callback (for example, named DML templates).
// name is a simple operation name, without the spaniel.storage. prefix.
func (d *DB) namedQuery(name string) *querygen.Query {
	return querygen.Use(d.namedGORM(name))
}

// namedGORM attaches a simple operation name using the shared storage prefix.
func (d *DB) namedGORM(name string) *gorm.DB {
	ctx := context.Background()
	if d.gorm.Statement != nil && d.gorm.Statement.Context != nil {
		ctx = d.gorm.Statement.Context
	}
	return d.gorm.WithContext(WithQueryName(ctx, name))
}

// The batcher pins one database/sql connection for each telemetry table,
// including events/links. Keep a small, bounded GORM lane for metadata and reads.
// One connection makes an alert-rule read wait behind any unrelated GORM work;
// an unbounded pool lets concurrent OTLP requests create many native DuckDB
// connections and exhaust the host.
const (
	// ManagedWALCheckpointBytes bounds WAL accumulated before a guarded
	// checkpoint and the freshness of a size-maintenance checkpoint phase.
	ManagedWALCheckpointBytes = int64(8 << 20)
	duckDBWorkerThreads       = 4
	duckDBMemoryLimit         = "2GB"
	duckDBGORMConnections     = 16
	// The guard schedules small WAL checkpoints under maintenance isolation.
	// Keep native commit-time checkpoints out of the ingestion path without
	// disabling WAL durability or checkpoint-on-shutdown.
	duckDBAutoCheckpointThreshold = "1TB"
)

var duckDBMaxOpenConnections = len(batchTables) + duckDBGORMConnections

func Open(path string) (*DB, error) {
	if err := recoverPhysicalSwap(path); err != nil {
		return nil, err
	}
	sqlPool, err := openSQLPool(path)
	if err != nil {
		return nil, err
	}
	pool := &switchablePool{db: sqlPool}
	g, err := gorm.Open(duckdb.New(duckdb.Config{Conn: pool}), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		_ = sqlPool.Close()
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, fmt.Errorf("duckdb pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(duckDBMaxOpenConnections)
	sqlDB.SetMaxIdleConns(duckDBMaxOpenConnections)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	d := &DB{gorm: g, pool: pool, maintenanceMu: &sync.Mutex{}, path: path, full: &atomic.Bool{}, traceLintMu: &sync.Mutex{}, retentionFloor: &atomic.Int64{}, retentionRetry: &atomic.Int64{}, retentionCheckpointReady: &atomic.Bool{}, retentionReclaimed: &atomic.Bool{}, retentionRebuildVisited: &atomic.Uint64{}, retentionPhysicalReady: &atomic.Bool{}, retentionPhysicalFreed: &atomic.Int64{}, retentionPhysicalRatio: &atomic.Uint64{}}
	if err := d.recoverReclaimIndexes(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("recover interrupted reclamation: %w", err)
	}
	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := d.initReclaimIndexSlots(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("initialize reclamation journal: %w", err)
	}
	_ = g.Use(newGORMPlugin())
	d.query = querygen.Use(g)
	if count, err := d.ActiveMetricSeries(); err == nil {
		telemetry.Catalog().SetActiveSeries(count)
	}
	registerDBSizeGauge(path)

	// Hot-path inserts (spans, logs, metrics) go through the columnar Appender
	// API rather than per-row INSERTs. Must run after migrate() so the tables
	// exist for the appenders to bind to.
	b, err := newBatcher(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("init batcher: %w", err)
	}
	d.batcher = b
	return d, nil
}

// duckDBDSN places resource limits in the connector configuration rather than
// issuing SET statements on one connection. database/sql opens connections
// lazily, so per-connection SETs leave later query connections unconstrained.
func duckDBDSN(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	// Avoid the legacy ART-index serialization path during checkpoint. The
	// bundled engine can read its current format; legacy conversion can fail
	// with TransformToDeprecated after sustained indexed ingestion/deletion.
	settings := fmt.Sprintf("threads=%d&memory_limit=%s&storage_compatibility_version=latest&checkpoint_threshold=%s", duckDBWorkerThreads, duckDBMemoryLimit, duckDBAutoCheckpointThreshold)
	// User SQL shares this instance with ingestion. Disable external I/O and
	// extension loading on the instance so SELECT table functions cannot bypass
	// the read-only SQL gate. All connections inherit the same configuration.
	settings += "&enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false&allow_community_extensions=false&lock_configuration=true"
	return path + sep + settings
}

// WALSize is a cheap filesystem snapshot for scheduling guarded checkpoints.
func (d *DB) WALSize() int64 {
	if d.path == "" || d.path == ":memory:" {
		return 0
	}
	info, err := os.Stat(d.path + ".wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

// FlushBatch flushes all buffered hot-path rows so they are visible to readers.
// Ingest groups call this after processing; other writers flush before readback.
func (d *DB) FlushBatch() error {
	err := d.batcher.Flush()
	if err == nil {
		registerDBSizeGauge(d.path)
	}
	return err
}

// BatchFailureVersion snapshots failures before a shared export group starts.
func (d *DB) BatchFailureVersion() uint64 {
	d.batcher.mu.Lock()
	defer d.batcher.mu.Unlock()
	return d.batcher.failureVersion
}

// FlushBatchSince prevents another explicit flush (for example a tail-sampling
// callback) from consuming a failure belonging to an executing export group.
func (d *DB) FlushBatchSince(version uint64) error {
	err := d.FlushBatch()
	d.batcher.mu.Lock()
	defer d.batcher.mu.Unlock()
	if d.batcher.failureVersion != version {
		return errors.Join(err, d.batcher.lastFailure)
	}
	return err
}

// SQL exposes the underlying *sql.DB for callers that need to run statements
// outside the curated API (seed fixtures, ad-hoc migrations). Prefer the
// typed methods above for anything in the hot path.
func (d *DB) SQL() *sql.DB {
	sqlDB, _ := d.gorm.DB()
	return sqlDB
}

func (d *DB) Close() error {
	// Flush and release appenders (and their pinned connections) before closing
	// the pool, so buffered rows are persisted on graceful shutdown.
	if d.batcher != nil {
		_ = d.batcher.Close()
	}
	sqlDB, err := d.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
