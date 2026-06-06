package storage

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

// ErrStorageFull signals that telemetry can't be stored because the database is
// at its size cap (and can't be pruned further) or the disk is out of space.
// Receivers map it to a retryable response (HTTP 503 / gRPC RESOURCE_EXHAUSTED).
var ErrStorageFull = errors.New("storage full")

// IsStorageFull reports whether err is (or wraps) a storage-full condition —
// our sentinel, an ENOSPC errno, or a DuckDB/OS out-of-space message.
func IsStorageFull(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStorageFull) || errors.Is(err, syscall.ENOSPC) {
		return true
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "no space left") ||
		strings.Contains(m, "disk is full") ||
		strings.Contains(m, "out of disk") ||
		strings.Contains(m, "enospc")
}

// Full reports whether the storage is currently full (writes rejected). It's an
// atomic snapshot maintained by the storage guard and ingest write failures.
func (d *DB) Full() bool { return d.full.Load() }

// SetFull updates the full state. Set true when the size cap is hit and can't be
// pruned, or a write fails for lack of space; false once space is available.
func (d *DB) SetFull(v bool) { d.full.Store(v) }

// FileSize returns the on-disk footprint of the database (main file + WAL), or
// 0 for an in-memory database.
func (d *DB) FileSize() int64 {
	if d.path == "" || d.path == ":memory:" {
		return 0
	}
	var total int64
	for _, suffix := range []string{"", ".wal"} {
		if fi, err := os.Stat(d.path + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}
