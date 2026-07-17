package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// ReadOnlyQuery runs query against a read-only DuckDB connection to the same
// database file and returns the result columns and rows (capped at maxRows;
// truncated reports whether more rows were available). []byte values are
// converted to strings so they marshal cleanly to JSON.
//
// Read-only is enforced by DuckDB itself: the connection attaches the database
// in read_only mode, so the engine rejects any write/DDL/COPY/ATTACH with an
// error, while reads of any kind (including multiple statements and
// DuckDB-specific syntax) run normally. A fresh connection is opened per call so
// results reflect the latest committed data (the server writes through a
// separate read-write handle).
//
// This requires a file-backed database; an in-memory database can't be reopened
// read-only as a second instance.
func (d *DB) ReadOnlyQuery(ctx context.Context, query string, maxRows int) (cols []string, rows [][]any, truncated bool, err error) {
	if maxRows <= 0 {
		maxRows = 1000
	}
	if d.path == "" || d.path == ":memory:" {
		return nil, nil, false, fmt.Errorf("read-only SQL is unavailable: the database is in-memory, not file-backed")
	}

	ro, err := sql.Open("duckdb", duckDBDSN(d.path, true))
	if err != nil {
		return nil, nil, false, fmt.Errorf("open read-only connection: %w", err)
	}
	defer ro.Close() //nolint:errcheck
	ro.SetMaxOpenConns(1)

	rs, err := ro.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, false, err
	}
	defer rs.Close()

	cols, err = rs.Columns()
	if err != nil {
		return nil, nil, false, err
	}

	for rs.Next() {
		if len(rows) >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, nil, false, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		rows = append(rows, vals)
	}
	if err := rs.Err(); err != nil {
		return nil, nil, false, err
	}
	return cols, rows, truncated, nil
}
