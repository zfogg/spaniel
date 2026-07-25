package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/marcboeker/go-duckdb"
)

// DuckDB is columnar and pays a heavy fixed cost per INSERT statement, so the
// ingestion hot path writes spans, logs, and metrics through the columnar
// Appender API instead. The Batcher keeps one long-lived appender per table
// (each pinned to its own connection from the shared pool) and flushes the
// buffered rows on three triggers: a row-count threshold, a background
// interval, and explicitly at the end of every ingest request. Low-frequency
// writes (sessions, trace_issues, lint_warnings, span_events, span_links) stay
// on GORM — only the per-record hot path moves here.
const (
	// batchMaxRows flushes an appender once it has buffered this many rows,
	// bounding memory and visibility latency for very large ingest requests.
	batchMaxRows = 4096
	// batchInterval is a liveness backstop: any pending rows are flushed at
	// least this often. Ingest calls flush at their end (see Pipeline), so in
	// practice this only fires for unusually long-running requests.
	batchInterval = 200 * time.Millisecond
)

var errBatcherClosed = errors.New("storage: batcher is closed")

// batchTables is the set of tables the Batcher manages, in a fixed order.
var batchTables = []string{"spans", "logs", "metrics"}

// tableAppender pairs a DuckDB appender with the connection it is pinned to and
// the number of rows buffered since the last flush. An appender is not safe for
// concurrent use; the owning Batcher serialises all access under its mutex.
type tableAppender struct {
	table   string
	conn    *sql.Conn
	app     *duckdb.Appender
	pending int
}

func (ta *tableAppender) close() error {
	var err error
	if ta.app != nil {
		err = ta.app.Close()
	}
	if ta.conn != nil {
		_ = ta.conn.Close()
	}
	return err
}

// Batcher buffers hot-path rows and flushes them via the DuckDB Appender API.
type Batcher struct {
	db   *sql.DB
	mu   sync.Mutex
	apps map[string]*tableAppender

	stop   chan struct{}
	done   chan struct{}
	closed bool
}

// newBatcher pins one appender per hot-path table and starts the interval
// flusher. The pinned connections share the same DuckDB instance as the GORM
// pool, so appended rows are visible to GORM reads after a flush.
func newBatcher(db *sql.DB) (*Batcher, error) {
	b := &Batcher{
		db:   db,
		apps: make(map[string]*tableAppender, len(batchTables)),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	for _, t := range batchTables {
		ta, err := newTableAppender(db, t)
		if err != nil {
			_ = b.closeAllLocked()
			return nil, fmt.Errorf("create appender for %s: %w", t, err)
		}
		b.apps[t] = ta
	}
	go b.loop()
	return b, nil
}

// newTableAppender pins a dedicated connection from the pool and opens an
// appender on it. The connection is held for the lifetime of the appender so
// database/sql never hands it to another caller or closes it underneath us.
func newTableAppender(db *sql.DB, table string) (*tableAppender, error) {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	ta := &tableAppender{table: table, conn: conn}
	err = conn.Raw(func(dc any) error {
		driverConn, ok := dc.(driver.Conn)
		if !ok {
			return fmt.Errorf("unexpected driver connection type %T", dc)
		}
		app, aerr := duckdb.NewAppenderFromConn(driverConn, "", table)
		if aerr != nil {
			return aerr
		}
		ta.app = app
		return nil
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ta, nil
}

func (b *Batcher) loop() {
	defer close(b.done)
	t := time.NewTicker(batchInterval)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			_ = b.Flush()
		}
	}
}

// append adds one row to the named table's appender, flushing eagerly once the
// row threshold is reached. A failure invalidates the appender, so it is
// recreated before returning the error; the row that triggered the failure is
// lost (callers account for it via drop counters), but the appender stays
// usable for subsequent rows.
func (b *Batcher) append(table string, args ...driver.Value) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errBatcherClosed
	}
	ta, err := b.appenderLocked(table)
	if err != nil {
		return err
	}
	if err := ta.app.AppendRow(args...); err != nil {
		b.recreateLocked(table)
		return fmt.Errorf("append %s: %w", table, err)
	}
	ta.pending++
	if ta.pending >= batchMaxRows {
		return b.flushTableLocked(ta)
	}
	return nil
}

// appenderLocked returns the appender for a table, recreating it if a previous
// error left it nil. Caller must hold b.mu.
func (b *Batcher) appenderLocked(table string) (*tableAppender, error) {
	ta := b.apps[table]
	if ta != nil && ta.app != nil {
		return ta, nil
	}
	b.recreateLocked(table)
	if ta = b.apps[table]; ta == nil || ta.app == nil {
		return nil, fmt.Errorf("storage: appender for %s unavailable", table)
	}
	return ta, nil
}

// recreateLocked closes and reopens a table's appender after an error. On
// failure the slot is left nil and the next append/flush retries. Caller must
// hold b.mu.
func (b *Batcher) recreateLocked(table string) {
	if old := b.apps[table]; old != nil {
		_ = old.close()
	}
	ta, err := newTableAppender(b.db, table)
	if err != nil {
		b.apps[table] = nil
		return
	}
	b.apps[table] = ta
}

// flushTableLocked flushes one appender's buffered rows. Caller must hold b.mu.
func (b *Batcher) flushTableLocked(ta *tableAppender) error {
	if ta == nil || ta.app == nil || ta.pending == 0 {
		return nil
	}
	if err := ta.app.Flush(); err != nil {
		b.recreateLocked(ta.table)
		return fmt.Errorf("flush %s: %w", ta.table, err)
	}
	ta.pending = 0
	return nil
}

// Flush flushes every appender's buffered rows so they become visible to
// readers. It is safe to call concurrently with append.
func (b *Batcher) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	var errs []error
	for _, t := range batchTables {
		if err := b.flushTableLocked(b.apps[t]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Maintenance temporarily closes all appenders while fn performs a DuckDB
// maintenance operation such as CHECKPOINT or VACUUM. DuckDB must not run
// those operations while an Appender is alive, even if it has been flushed:
// doing so can invalidate the database with "Invalid node type" errors.
// Appends are blocked for the duration and fresh appenders are installed
// before this method returns.
func (b *Batcher) Maintenance(fn func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fn()
	}

	closeErr := b.closeAllLocked()
	fnErr := fn()

	var reopenErrs []error
	for _, table := range batchTables {
		ta, err := newTableAppender(b.db, table)
		if err != nil {
			reopenErrs = append(reopenErrs, fmt.Errorf("reopen appender for %s: %w", table, err))
			continue
		}
		b.apps[table] = ta
	}
	return errors.Join(closeErr, fnErr, errors.Join(reopenErrs...))
}

// Close stops the interval flusher, flushes remaining rows, and releases the
// pinned connections. It is idempotent.
func (b *Batcher) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	close(b.stop)
	<-b.done // wait for the interval flusher to exit before closing appenders

	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeAllLocked()
}

// closeAllLocked closes every appender (which flushes any remaining rows) and
// clears the map. Caller must hold b.mu.
func (b *Batcher) closeAllLocked() error {
	var errs []error
	for _, t := range batchTables {
		if ta := b.apps[t]; ta != nil {
			if err := ta.close(); err != nil {
				errs = append(errs, err)
			}
			b.apps[t] = nil
		}
	}
	return errors.Join(errs...)
}

// AppendSpan buffers a span for batched insertion.
func (b *Batcher) AppendSpan(s *Span) error {
	if s.DurationNs == 0 {
		s.DurationNs = s.EndNs - s.StartNs
	}
	return b.append("spans",
		s.TraceID, s.SpanID, s.ParentSpanID, s.ServiceName, s.Name,
		s.Kind, s.StartNs, s.EndNs, s.DurationNs,
		s.StatusCode, s.StatusMessage, s.Attributes, s.Resource,
		s.SessionID, s.SessionLabel, s.ReceivedAt, s.Sampled,
	)
}

// AppendLog buffers a log record for batched insertion.
func (b *Batcher) AppendLog(l *Log) error {
	return b.append("logs",
		l.TimestampNs, l.TraceID, l.SpanID, l.Severity, l.Body,
		l.Attributes, l.ServiceName, l.SessionID, l.ReceivedAt,
	)
}

// AppendMetric buffers a metric data point for batched insertion.
func (b *Batcher) AppendMetric(m *Metric) error {
	return b.append("metrics",
		m.Name, m.Description, m.Unit, m.Type, m.TimestampNs,
		m.Value, m.Attributes, m.Exemplars, m.ServiceName, m.SessionID,
	)
}
