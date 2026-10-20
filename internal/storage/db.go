package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alifiroozi80/duckdb"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"github.com/zfogg/spaniel/internal/telemetry"
	"golang.org/x/sync/semaphore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	gorm               *gorm.DB
	query              *querygen.Query
	batcher            *Batcher
	path               string
	activeSessionID    string
	activeSessionLabel string
	full               *atomic.Bool // writes rejected when true (see full.go); shared across WithContext copies
	ingestGate         *semaphore.Weighted
}

// WithContext returns a shallow copy of DB whose GORM queries carry ctx, so the
// OTel plugin (see otel.go) nests DuckDB query spans under the caller's span.
// Use it on request paths: store.WithContext(req.Context()).ListTraces(...).
// Batcher writes bypass GORM instrumentation, but honor ctx while queued for
// their native write/flush. Native work already executing finishes safely.
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

// The batcher pins one database/sql connection for each of spans, logs, and
// metrics. Keep a small, bounded GORM lane for metadata and read queries.
// One connection makes an alert-rule read wait behind any unrelated GORM work;
// an unbounded pool lets concurrent OTLP requests create many native DuckDB
// connections and exhaust the host.
const (
	duckDBWorkerThreads   = 4
	duckDBMemoryLimit     = "2GB"
	duckDBGORMConnections = 16
)

var duckDBMaxOpenConnections = len(batchTables) + duckDBGORMConnections

func Open(path string) (*DB, error) {
	g, err := gorm.Open(duckdb.Open(duckDBDSN(path, false)), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
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
	d := &DB{gorm: g, path: path, full: &atomic.Bool{}, ingestGate: semaphore.NewWeighted(ingestGateCapacity)}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
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
	b, err := newBatcher(sqlDB, path, func() error {
		return d.flushBeforeCheckpoint()
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("init batcher: %w", err)
	}
	d.batcher = b
	go b.loop()
	return d, nil
}

// DuckDB's appenders share one native write lane. Queue complete exports,
// rather than interleaving their rows and flushing each other's partial batches.
const ingestGateCapacity int64 = 1

// BeginIngest protects a complete export, rather than just one appended row.
// Other exports and maintenance wait for the active export to finish. Waiting
// exports remain cancellable and cannot monopolize the native appender queue.
func (d *DB) BeginIngest(ctx context.Context) (func(), error) {
	if err := d.ingestGate.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { d.ingestGate.Release(1) }, nil
}

func (d *DB) pauseIngest(fn func() error) error {
	ctx, cancel := context.WithTimeout(d.gorm.Statement.Context, 30*time.Second)
	defer cancel()
	if err := d.ingestGate.Acquire(ctx, ingestGateCapacity); err != nil {
		return fmt.Errorf("drain ingestion for maintenance: %w", err)
	}
	defer d.ingestGate.Release(ingestGateCapacity)
	return fn()
}

// duckDBDSN places resource limits in the connector configuration rather than
// issuing SET statements on one connection. database/sql opens connections
// lazily, so per-connection SETs leave later query connections unconstrained.
func duckDBDSN(path string, readOnly bool) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	settings := fmt.Sprintf("threads=%d&memory_limit=%s", duckDBWorkerThreads, duckDBMemoryLimit)
	if !readOnly {
		// Commit-triggered checkpoints bypass Batcher.Maintenance and can run
		// while another table still has a live native appender. The batcher's
		// WAL watcher performs checkpoints with all appenders closed instead.
		settings += "&debug_skip_checkpoint_on_commit=true"
	}
	if readOnly {
		// User-authored dashboard and alert SQL runs on this connection. Keep
		// DuckDB from reaching the network/filesystem or loading extensions even
		// when a query uses otherwise read-looking SQL constructs.
		settings += "&access_mode=read_only&enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false&allow_community_extensions=false&lock_configuration=true"
	}
	return path + sep + settings
}

// FlushBatch flushes all buffered hot-path rows so they are visible to readers.
// Ingest paths call this at the end of a request and before running detectors.
func (d *DB) FlushBatch() error {
	err := d.batcher.flush(d.gorm.Statement.Context)
	if err == nil {
		registerDBSizeGauge(d.path)
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
