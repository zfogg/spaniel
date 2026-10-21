package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/marcboeker/go-duckdb"
	"github.com/zfogg/spaniel/internal/telemetry"
)

// DuckDB is columnar and pays a heavy fixed cost per INSERT statement, so the
// ingestion hot path writes spans, events, links, logs, and metrics through the columnar
// Appender API instead. The Batcher keeps one long-lived appender per table
// (each pinned to its own connection from the shared pool) and flushes the
// buffered rows on three triggers: a row-count threshold, a background
// interval, and explicitly at the end of each shared ingestion group. Low-frequency
// writes (sessions, trace_issues, lint_warnings) stay
// on GORM — only the per-record hot path moves here.
const (
	// batchMaxRows flushes an appender once it has buffered this many rows,
	// bounding memory and visibility latency for very large ingest requests.
	batchMaxRows = 4096
	// batchInterval is a liveness backstop: any pending rows are flushed at
	// least this often once processing has appended them. Shared ingest groups
	// flush explicitly at completion; the interval also protects long groups.
	batchInterval = 200 * time.Millisecond
)

var errBatcherClosed = errors.New("storage: batcher is closed")

// batchTables is the set of tables the Batcher manages, in a fixed order.
var batchTables = []string{"spans", "span_events", "span_links", "logs", "metrics"}

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
	err := ta.closeNative()
	if ta.conn != nil {
		_ = ta.conn.Close()
		ta.conn = nil
	}
	return err
}

// closeNative flushes and destroys the native appender without returning its
// connection lease to the SQL pool. Maintenance must keep that lease reserved
// so queued readers cannot prevent ingestion from being restored.
func (ta *tableAppender) closeNative() error {
	app := ta.app
	ta.app = nil
	ta.pending = 0
	if app != nil {
		return app.Close()
	}
	return nil
}

func (ta *tableAppender) reopen() error {
	if ta.app != nil {
		return nil
	}
	return ta.conn.Raw(func(dc any) error {
		driverConn, ok := dc.(driver.Conn)
		if !ok {
			return fmt.Errorf("unexpected driver connection type %T", dc)
		}
		app, err := duckdb.NewAppenderFromConn(driverConn, "", ta.table)
		if err != nil {
			return err
		}
		ta.app = app
		return nil
	})
}

// Batcher buffers hot-path rows and flushes them via the DuckDB Appender API.
type Batcher struct {
	db   *sql.DB
	mu   sync.Mutex
	apps map[string]*tableAppender

	stop   chan struct{}
	done   chan struct{}
	closed bool
	// Keep background/threshold failures until an explicit acknowledgment
	// barrier observes them. A later successful flush cannot hide lost rows.
	flushErr       error
	failureVersion uint64
	lastFailure    error
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
	err = ta.reopen()
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
			_ = b.flush(false)
		}
	}
}

// append adds one row to the named table's appender, flushing eagerly once the
// row threshold is reached. A failure invalidates the appender, so it is
// recreated before returning the error; the row that triggered the failure is
// lost (callers account for it via drop counters), but the appender stays
// usable for subsequent rows.
func (b *Batcher) append(table string, args ...driver.Value) error {
	started := time.Now()
	defer func() {
		telemetry.Catalog().RecordStorageAppend(context.Background(), table, float64(time.Since(started).Microseconds())/1000)
	}()
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
		b.failLocked(fmt.Errorf("append %s: %w", table, err))
		b.recreateLocked(table)
		return fmt.Errorf("append %s: %w", table, err)
	}
	ta.pending++
	b.recordQueueDepthLocked()
	telemetry.Catalog().RecordStorageBytesWritten(context.Background(), estimateRowBytes(args))
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
	pending := ta.pending
	started := time.Now()
	if err := ta.app.Flush(); err != nil {
		b.failLocked(fmt.Errorf("flush %s: %w", ta.table, err))
		b.recreateLocked(ta.table)
		telemetry.Catalog().RecordStorage(context.Background(), "write", "error", int64(pending), float64(time.Since(started).Microseconds())/1000)
		telemetry.Catalog().RecordStorageFlush(context.Background(), ta.table, "error", int64(pending))
		return fmt.Errorf("flush %s: %w", ta.table, err)
	}
	ta.pending = 0
	b.recordQueueDepthLocked()
	telemetry.Catalog().RecordStorage(context.Background(), "write", "ok", int64(pending), float64(time.Since(started).Microseconds())/1000)
	telemetry.Catalog().RecordStorageFlush(context.Background(), ta.table, "ok", int64(pending))
	return nil
}

func (b *Batcher) recordQueueDepthLocked() {
	var pending int
	for _, ta := range b.apps {
		if ta != nil {
			pending += ta.pending
		}
	}
	telemetry.Catalog().SetStorageWriteQueueDepth(int64(pending))
}

func estimateRowBytes(args []driver.Value) int64 {
	var total int64
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			total += int64(len(v))
		case []byte:
			total += int64(len(v))
		case json.RawMessage:
			total += int64(len(v))
		case int64, uint64, float64:
			total += 8
		case int32, uint32, float32:
			total += 4
		case bool:
			total++
		case nil:
		default:
			total += int64(len(fmt.Sprint(v)))
		}
	}
	return total
}

// Flush flushes every appender's buffered rows so they become visible to
// readers. It is safe to call concurrently with append.
func (b *Batcher) Flush() error {
	return b.flush(true)
}

func (b *Batcher) flush(acknowledge bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	for _, t := range batchTables {
		_ = b.flushTableLocked(b.apps[t])
	}
	err := b.flushErr
	if acknowledge {
		b.flushErr = nil
	}
	return err
}

// Maintenance temporarily closes all appenders while fn performs a DuckDB
// maintenance operation such as CHECKPOINT or VACUUM. DuckDB must not run
// those operations while an Appender is alive, even if it has been flushed:
// doing so can invalidate the database with "Invalid node type" errors.
// Appends are blocked for the duration. Connection leases remain pinned while
// native appenders are closed, and fresh appenders are restored on those same
// leases before unlocking, including when fn fails or ctx is canceled. fn is
// told how many pool connections remain reserved for restoration.
func (b *Batcher) Maintenance(ctx context.Context, fn func(pinned int) error) (err error) {
	if err := lockWithContext(ctx, &b.mu); err != nil {
		return err
	}
	defer b.mu.Unlock()
	if b.closed {
		// Close publishes closed before joining the interval flusher. It may
		// still be waiting to reacquire this mutex and release its appenders.
		// Finish that release before reporting zero reserved connections.
		closeErr := b.closeAllLocked()
		return errors.Join(closeErr, fn(0))
	}

	var closeErrs []error
	defer func() {
		var reopenErrs []error
		for _, table := range batchTables {
			ta := b.apps[table]
			if ta == nil || ta.conn == nil {
				continue
			}
			// Restoration deliberately ignores the canceled work context: these
			// leases are already owned, so reopening never waits on the pool.
			if e := ta.reopen(); e != nil {
				failure := fmt.Errorf("reopen appender for %s: %w", table, e)
				reopenErrs = append(reopenErrs, failure)
				b.failLocked(failure)
			}
		}
		b.recordQueueDepthLocked()
		err = errors.Join(errors.Join(closeErrs...), err, errors.Join(reopenErrs...))
	}()

	// Recover any already-missing lease before suspending healthy appenders.
	// Acquisition is bounded, and an early failure leaves those appenders open.
	pinned := 0
	for _, table := range batchTables {
		ta := b.apps[table]
		if ta == nil || ta.conn == nil {
			conn, e := b.db.Conn(ctx)
			if e != nil {
				return fmt.Errorf("reserve appender connection for %s: %w", table, e)
			}
			ta = &tableAppender{table: table, conn: conn}
			b.apps[table] = ta
		}
		pinned++
	}
	for _, table := range batchTables {
		if e := b.apps[table].closeNative(); e != nil {
			failure := fmt.Errorf("close appender for %s: %w", table, e)
			closeErrs = append(closeErrs, failure)
			b.failLocked(failure)
		}
	}
	b.recordQueueDepthLocked()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(pinned)
}

// Expired automatic maintenance must not begin another ingestion pause after
// waiting for an earlier writer/maintenance operation to release its lock.
func lockWithContext(ctx context.Context, mu *sync.Mutex) error {
	try := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !mu.TryLock() {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			mu.Unlock()
			return false, err
		}
		return true, nil
	}
	if acquired, err := try(); acquired || err != nil {
		return err
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if acquired, err := try(); acquired || err != nil {
				return err
			}
		}
	}
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

// AppendSpanEvents uses a pinned appender, so event-bearing OTLP batches do
// not wait for a general-purpose SQL connection occupied by dashboard reads.
func (b *Batcher) AppendSpanEvents(events []*SpanEvent) error {
	for _, e := range events {
		// These two legacy tables retain JSON columns. The DuckDB appender
		// JSON-marshals values for them; RawMessage preserves the OTLP object
		// rather than wrapping its serialized representation in a JSON string.
		if err := b.append("span_events", e.SpanID, e.TraceID, e.SessionID, e.TimeNs, e.Name, json.RawMessage(e.Attributes)); err != nil {
			return err
		}
	}
	return nil
}

// AppendSpanLinks shares the same flush/maintenance lifecycle as span rows.
func (b *Batcher) AppendSpanLinks(links []*SpanLink) error {
	for _, l := range links {
		if err := b.append("span_links", l.SpanID, l.TraceID, l.SessionID, l.LinkedTraceID, l.LinkedSpanID, l.TraceState, json.RawMessage(l.Attributes)); err != nil {
			return err
		}
	}
	return nil
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
		m.Name, m.Description, m.Unit, m.Type, m.AggregationTemporality, nullableBool(m.IsMonotonic),
		m.StartTimestampNs, m.TimestampNs, m.Flags, m.Value,
		nullableUint64(m.HistogramCount), nullableFloat64(m.HistogramSum), nullableFloat64(m.HistogramMin), nullableFloat64(m.HistogramMax), m.ExplicitBounds, m.BucketCounts,
		nullableInt32(m.ExpScale), nullableUint64(m.ExpZeroCount), nullableFloat64(m.ExpZeroThreshold), nullableInt32(m.ExpPositiveOffset), m.ExpPositiveCounts, nullableInt32(m.ExpNegativeOffset), m.ExpNegativeCounts,
		nullableUint64(m.SummaryCount), nullableFloat64(m.SummarySum), m.SummaryQuantiles,
		m.Attributes, m.Resource, m.SeriesAttributes, m.SeriesKey, m.ScopeName, m.ScopeVersion, m.ScopeSchemaURL, m.ScopeAttributes,
		m.Exemplars, m.ServiceName, m.SessionID,
	)
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableUint64(v *uint64) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullableFloat64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullableInt32(v *int32) any {
	if v == nil {
		return nil
	}
	return *v
}

func (b *Batcher) failLocked(err error) {
	b.failureVersion++
	b.lastFailure = err
	b.flushErr = errors.Join(b.flushErr, err)
}
