package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Keep the GORM/query handles stable while a private, drained pool is replaced.
// Holding mu excludes new submissions; closing the old SQL pool joins existing
// rows and transactions before any file is renamed. Appenders have their own
// mutex and are restored before the submission gate reopens.
type switchablePool struct {
	mu sync.RWMutex
	db *sql.DB
}

func (p *switchablePool) GetDBConn() (*sql.DB, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db, nil
}
func (p *switchablePool) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db.PrepareContext(ctx, q)
}
func (p *switchablePool) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db.ExecContext(ctx, q, args...)
}
func (p *switchablePool) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db.QueryContext(ctx, q, args...)
}
func (p *switchablePool) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db.QueryRowContext(ctx, q, args...)
}
func (p *switchablePool) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db.BeginTx(ctx, opts)
}

func openSQLPool(path string) (*sql.DB, error) {
	p, err := sql.Open("duckdb", duckDBDSN(path))
	if err != nil {
		return nil, err
	}
	p.SetMaxOpenConns(duckDBMaxOpenConnections)
	p.SetMaxIdleConns(duckDBMaxOpenConnections)
	if err = p.Ping(); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

// The old file remains authoritative until the complete candidate is synced.
// A crash between the two renames restores the old file on the next open.
func recoverPhysicalSwap(path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}
	old := path + ".repack-old"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(old); err == nil {
			return os.Rename(old, path)
		}
	}
	return nil
}

func syncFile(path string) error {
	// Windows FlushFileBuffers requires a handle opened with write access.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// rewritePhysicalLocked runs with all maintenance serialized. Native copying
// moves live contents, schema, sequences and indexes to a fresh physical file;
// a checkpoint alone cannot remove holes inside the old single-file database.
func (d *DB) rewritePhysicalLocked() (err error) {
	if d.path == "" || d.path == ":memory:" {
		return nil
	}
	b := d.batcher
	if b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.closed {
			return errBatcherClosed
		}
		if err = b.closeAllLocked(); err != nil {
			for _, table := range batchTables {
				ta, e := newTableAppender(b.db, table)
				if e != nil {
					err = errors.Join(err, e)
				} else {
					b.apps[table] = ta
				}
			}
			b.failLocked(err)
			return err
		}
	}
	d.pool.mu.Lock()
	defer d.pool.mu.Unlock()
	// Own every SQL lease before closing the instance. sql.DB.Close alone can
	// leave a native connection alive while its caller still owns Rows/Tx.
	ctx, cancel := context.WithTimeout(d.gorm.Statement.Context, 30*time.Second)
	defer cancel()
	var leases []*sql.Conn
	for range d.pool.db.Stats().MaxOpenConnections {
		conn, e := d.pool.db.Conn(ctx)
		if e != nil {
			for _, c := range leases {
				_ = c.Close()
			}
			// The old pool remains open; restore appenders on it before unblocking.
			if b != nil {
				for _, table := range batchTables {
					ta, reopenErr := newTableAppender(b.db, table)
					if reopenErr != nil {
						b.failLocked(reopenErr)
						e = errors.Join(e, reopenErr)
					} else {
						b.apps[table] = ta
					}
				}
			}
			return e
		}
		leases = append(leases, conn)
	}
	for _, conn := range leases {
		_ = conn.Close()
	}
	// Recovery always reopens the authoritative path and restores every appender,
	// including copy/rename failures. No handle points to a detached old inode.
	defer func() {
		p, e := openSQLPool(d.path)
		if e != nil {
			err = errors.Join(err, e)
			d.SetFull(true)
			return
		}
		d.pool.db = p
		if b != nil {
			b.db = p
			for _, table := range batchTables {
				ta, e := newTableAppender(p, table)
				if e != nil {
					err = errors.Join(err, e)
					b.failLocked(e)
					continue
				}
				b.apps[table] = ta
			}
		}
		if err == nil {
			_ = os.Remove(d.path + ".repack-old")
		}
	}()
	if err = d.pool.db.Close(); err != nil {
		return err
	}
	work, e := os.MkdirTemp(filepath.Dir(d.path), ".spaniel-repack-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(work)
	candidate := filepath.Join(work, filepath.Base(d.path))
	old := d.path + ".repack-old"
	// Only this private connection permits file I/O. The serving pool keeps its
	// external-access/extension restrictions and is closed throughout the copy.
	if err = copyPhysicalDatabase(d.path, candidate); err != nil {
		return err
	}
	// Catalog-qualified indexes in this DuckDB version require the same base
	// filename. Verify opening under the restricted serving configuration before
	// the authoritative file is touched.
	verified, e := openSQLPool(candidate)
	if e != nil {
		return fmt.Errorf("validate physical rewrite: %w", e)
	}
	if e = verified.Close(); e != nil {
		return e
	}
	if err = syncFile(candidate); err != nil {
		return err
	}
	if err = os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Rename(d.path, old); err != nil {
		return err
	}
	if err = os.Rename(candidate, d.path); err != nil {
		return errors.Join(err, os.Rename(old, d.path))
	}
	// Directory syncing makes the rename durable on UNIX. Windows does not
	// support directory Sync; both files still remain recoverable across restart.
	if dir, e := os.Open(filepath.Dir(d.path)); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// PrunePhysical enforces the cap on stat(main)+stat(WAL), not reusable blocks.
// Repack before deleting anything: fragmentation must not cost live telemetry.
// Then delete a bounded oldest prefix, repack and measure, repeating only while
// the last batch made physical progress. A metadata floor fails closed.
func (d *DB) PrunePhysical(cfg RetentionConfig, activeID string) (res PruneResult, err error) {
	if cfg.MaxDBSizeBytes <= 0 {
		return d.Prune(cfg, activeID)
	}
	ageCount := cfg
	ageCount.MaxDBSizeBytes = 0
	res, err = d.Prune(ageCount, activeID)
	if err != nil {
		return res, err
	}
	if d.FileSize() <= cfg.MaxDBSizeBytes {
		d.retentionPhysicalReady.Store(false)
		return res, nil
	}
	// Admission belongs to the guard: proactive compaction is not a full store.
	if cfg.MaxDuration > 0 && d.RetentionBackingOff() {
		return res, fmt.Errorf("%w: automatic physical reclamation backing off", ErrRetentionTargetUnreached)
	}
	defer func() {
		if err != nil {
			// A failed copy can leave committed tombstones in the authoritative
			// file. Retry their reclamation before allowing another live eviction.
			d.retentionPhysicalReady.Store(false)
		}
		if cfg.MaxDuration > 0 && IsRetentionTargetUnreached(err) {
			d.retentionRetry.Store(time.Now().Add(retentionRetryDelay).UnixNano())
		}
	}()
	d.maintenanceMu.Lock()
	defer d.maintenanceMu.Unlock()
	defer func() { res.FinalDBSizeBytes = d.FileSize() }()
	if cfg.MaxDuration == 0 || !d.retentionPhysicalReady.Load() {
		d.retentionPhysicalFreed.Store(0)
		d.retentionPhysicalRatio.Store(0)
		if err = d.rewritePhysicalLocked(); err != nil {
			return res, err
		}
		d.retentionPhysicalReady.Store(d.FileSize() > cfg.MaxDBSizeBytes)
		if cfg.MaxDuration > 0 {
			// One physical copy per automatic pass. Reopen serving handles and
			// give ingestion a window before any eviction/copy phase follows.
			return res, nil
		}
	}
	for d.FileSize() > cfg.MaxDBSizeBytes {
		if err = d.gorm.Statement.Context.Err(); err != nil {
			return res, err
		}
		before := d.FileSize()
		fraction := float64(before-cfg.MaxDBSizeBytes) / float64(before)
		if freed := d.retentionPhysicalFreed.Load(); freed > 0 {
			// Counts are not bytes: an old metric can be much smaller than a
			// recent log. Calibrate with the last measured copy instead of
			// repeating dozens of tiny full-database rewrites on that mix.
			last := math.Float64frombits(d.retentionPhysicalRatio.Load())
			fraction = max(fraction, float64(before-cfg.MaxDBSizeBytes)*last/float64(freed))
		}
		fraction = min(0.5, max(0.1, fraction))
		n := 0
		err = d.withMaintenanceLocked(func(isolated *DB) error {
			var e error
			n, e = isolated.deleteOldestTelemetryBatch(activeID, -fraction)
			return e
		})
		res.DeletedBySize += n
		if err != nil {
			return res, err
		}
		if n == 0 {
			return res, fmt.Errorf("%w: no evictable telemetry; physical bytes=%d", ErrRetentionTargetUnreached, before)
		}
		if err = d.rewritePhysicalLocked(); err != nil {
			return res, err
		}
		after := d.FileSize()
		slog.Info("physical retention batch", "deleted", n, "before_bytes", before, "after_bytes", after, "target_bytes", cfg.MaxDBSizeBytes)
		if after >= before {
			return res, fmt.Errorf("%w: physical rewrite made no progress; refusing further deletion", ErrRetentionTargetUnreached)
		}
		d.retentionPhysicalFreed.Store(before - after)
		d.retentionPhysicalRatio.Store(math.Float64bits(fraction))
		d.retentionPhysicalReady.Store(after > cfg.MaxDBSizeBytes)
		if cfg.MaxDuration > 0 {
			return res, nil
		}
	}
	d.retentionPhysicalReady.Store(false)
	return res, nil
}
