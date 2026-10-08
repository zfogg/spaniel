package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alifiroozi80/duckdb"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"github.com/zfogg/spaniel/internal/telemetry"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
func (d *DB) namedQuery(name string) *querygen.Query {
	return querygen.Use(d.namedGORM(name))
}

func (d *DB) namedGORM(name string) *gorm.DB {
	ctx := context.Background()
	if d.gorm.Statement != nil && d.gorm.Statement.Context != nil {
		ctx = d.gorm.Statement.Context
	}
	return d.gorm.WithContext(WithQueryName(ctx, name))
}

type Span = model.Span
type CoverageOperation = model.CoverageOperation
type CoverageQuality = model.CoverageQuality
type Log = model.Log
type Session = model.Session
type LintWarning = model.LintWarning
type TraceIssue = model.TraceIssue
type SpanEvent = model.SpanEvent
type SpanLink = model.SpanLink
type Metric = model.Metric
type NotificationRecord = model.NotificationRecord

// TraceRow and Stats are API projections rather than persisted schema models.
// They stay in storage so callers retain the existing public result types while
// generated DAOs own only actual database tables.
type TraceRow struct {
	TraceID       string   `json:"trace_id"`
	ServiceName   string   `json:"service_name"`
	Name          string   `json:"name"`
	Attributes    string   `json:"attributes"`
	StatusCode    int      `json:"status_code"`
	StartNs       int64    `json:"start_ns"`
	EndNs         int64    `json:"end_ns"`
	DurationNs    int64    `json:"duration_ns"`
	SessionID     string   `json:"session_id"`
	SessionLabel  string   `json:"session_label"`
	HasN1         bool     `json:"has_n1"`
	SpanCount     int      `json:"span_count"`
	IssueKinds    []string `json:"issue_kinds" gorm:"-"`
	IssueKindsRaw string   `json:"-" gorm:"column:issue_kinds_raw"`
}

type SourceStats struct {
	Service        string  `json:"service"`
	AcceptedPerSec float64 `json:"accepted_per_sec"`
	RejectedPerSec float64 `json:"rejected_per_sec"`
	ErrorRate      float64 `json:"error_rate"`
	BytesPerSec    float64 `json:"bytes_per_sec"`
	LastSeenNs     int64   `json:"last_seen_ns"`
}

type Stats struct {
	SpanCount           int   `json:"span_count"`
	TraceCount          int   `json:"trace_count"`
	LogCount            int   `json:"log_count"`
	DBSize              int64 `json:"db_size"`
	SessionCount        int   `json:"session_count"`
	OldestSessionAt     int64 `json:"oldest_session_at"`
	DroppedSpans        int64 `json:"dropped_spans"`
	DroppedLogs         int64 `json:"dropped_logs"`
	DroppedMetricPoints int64 `json:"dropped_metric_points"`
	LastDropAt          int64 `json:"last_drop_at"`
	StorageFull         bool  `json:"storage_full"`
	Throughput
}

type Throughput struct {
	SpansPerSec     float64 `json:"spans_per_sec"`
	LogsPerSec      float64 `json:"logs_per_sec"`
	MetricsPerSec   float64 `json:"metrics_per_sec"`
	PeakSpansPerSec float64 `json:"peak_spans_per_sec"`
}

// MetricCatalogEntry summarizes one (name, service) metric stream.
type MetricCatalogEntry = model.MetricCatalogEntry

type ServiceMapNode = model.ServiceMapNode
type ServiceMapOpStat = model.ServiceMapOpStat
type ServiceMapEdge = model.ServiceMapEdge

type ServiceMapData struct {
	Nodes []*ServiceMapNode `json:"nodes"`
	Edges []*ServiceMapEdge `json:"edges"`
}

// spanCols is the read projection for spans: JSON columns are cast to VARCHAR
// so they scan into the string fields on Span.
const spanCols = `trace_id, span_id, parent_span_id, service_name, name, kind,
	start_ns, end_ns, duration_ns, status_code, status_message,
	attributes::VARCHAR AS attributes, resource::VARCHAR AS resource,
	session_id, session_label, received_at`

// The batcher pins one database/sql connection for each of spans, logs, and
// metrics. Keep exactly one additional connection for GORM metadata and read
// queries. Leaving the pool unbounded lets concurrent OTLP requests create
// many native DuckDB connections; each connection can provision a CPU-sized
// worker set, producing thousands of OS threads and exhausting the host.
const (
	duckDBWorkerThreads = 4
	duckDBMemoryLimit   = "1GB"
)

var duckDBMaxOpenConnections = len(batchTables) + 1

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
	d := &DB{gorm: g, path: path, full: &atomic.Bool{}}
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
func duckDBDSN(path string, readOnly bool) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	settings := fmt.Sprintf("threads=%d&memory_limit=%s", duckDBWorkerThreads, duckDBMemoryLimit)
	if readOnly {
		// User-authored dashboard and alert SQL runs on this connection. Keep
		// DuckDB from reaching the network/filesystem or loading extensions even
		// when a query uses otherwise read-looking SQL constructs.
		settings += "&access_mode=read_only&enable_external_access=false&autoinstall_known_extensions=false&autoload_known_extensions=false&allow_community_extensions=false&lock_configuration=true"
	}
	return path + sep + settings
}

// AppendSpan buffers a span for batched insertion via the columnar Appender.
// Rows become durable on the next flush; ingest callers flush at the end of
// each request, so reads-after-ingest within a request observe the row.
func (d *DB) AppendSpan(s *Span) error { return d.batcher.AppendSpan(s) }

// AppendLog buffers a log record for batched insertion.
func (d *DB) AppendLog(l *Log) error { return d.batcher.AppendLog(l) }

// AppendMetric buffers a metric data point for batched insertion.
func (d *DB) AppendMetric(m *Metric) error { return d.batcher.AppendMetric(m) }

// MetricSeriesCatalog is the ingestion-facing view of durable admitted series.
type MetricSeriesCatalog struct {
	SessionID  string
	Service    string
	Name       string
	Attributes string
}

func (d *DB) RecordMetricSeries(sessionID, service, name, attrs string, timestampNs int64) (bool, error) {
	entry := &model.MetricSeriesCatalog{
		SessionID: sessionID, ServiceName: service, Name: name,
		SeriesKey: name + "\x00" + service + "\x00" + attrs, SeriesAttributes: attrs,
		FirstTimestampNs: timestampNs, LastTimestampNs: timestampNs, PointCount: 1,
	}
	if err := d.namedQuery("storage.RecordMetricSeries").MetricSeriesCatalog.Clauses(clause.OnConflict{DoNothing: true}).Create(entry); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) MetricSeriesCatalog() ([]MetricSeriesCatalog, error) {
	rows, err := d.query.MetricSeriesCatalog.Find()
	if err != nil {
		return nil, err
	}
	out := make([]MetricSeriesCatalog, len(rows))
	for i, row := range rows {
		out[i] = MetricSeriesCatalog{SessionID: row.SessionID, Service: row.ServiceName, Name: row.Name, Attributes: row.SeriesAttributes}
	}
	return out, nil
}

// FlushBatch flushes all buffered hot-path rows so they are visible to readers.
// Ingest paths call this at the end of a request and before running detectors.
func (d *DB) FlushBatch() error {
	err := d.batcher.Flush()
	if err == nil {
		registerDBSizeGauge(d.path)
	}
	return err
}

func (d *DB) CreateSession(label string, isBaseline bool) (*Session, error) {
	return d.createSession(label, isBaseline, false)
}

func (d *DB) CreateImportedSession(label string) (*Session, error) {
	if label == "" {
		label = fmt.Sprintf("import_%d", time.Now().UnixMilli())
	}
	return d.createSession(label, true, true)
}

func (d *DB) createSession(label string, isBaseline, isImported bool) (*Session, error) {
	now := time.Now().UnixNano()
	id := uuid.New().String()
	if label == "" {
		label = id
	}
	services, _ := json.Marshal([]string{})
	s := &Session{
		ID: id, Label: label, CreatedAt: now,
		IsBaseline: isBaseline, IsImported: isImported,
		SpanCount: 0, Services: string(services),
	}
	if err := d.namedQuery("storage.CreateSession").Session.Create(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) SetActiveSession(id, label string) {
	d.activeSessionID = id
	d.activeSessionLabel = label
}

// SQL exposes the underlying *sql.DB for callers that need to run statements
// outside the curated API (seed fixtures, ad-hoc migrations). Prefer the
// typed methods above for anything in the hot path.
func (d *DB) SQL() *sql.DB {
	sqlDB, _ := d.gorm.DB()
	return sqlDB
}

func (d *DB) ActiveSessionID() string    { return d.activeSessionID }
func (d *DB) ActiveSessionLabel() string { return d.activeSessionLabel }

func (d *DB) InsertSpan(s *Span) error {
	if s.DurationNs == 0 {
		s.DurationNs = s.EndNs - s.StartNs
	}
	return d.namedQuery("storage.InsertSpan").Span.Create(s)
}

// InsertSpanEvents bulk-inserts the events attached to a span. Empty input
// is a no-op; on error any rows inserted before the failure are not rolled
// back (events are best-effort, not transactional, like spans).
func (d *DB) InsertSpanEvents(events []*SpanEvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.namedQuery("storage.InsertSpanEvents").SpanEvent.Create(events...)
}

// ListEventsBySpan returns the events attached to a single span, in time order.
func (d *DB) ListEventsBySpan(spanID string) ([]*SpanEvent, error) {
	rows, err := d.query.SpanEvent.ListBySpan(spanID)
	if err != nil {
		return nil, err
	}
	out := make([]*SpanEvent, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

// InsertSpanLinks bulk-inserts links emitted by a span. Best-effort: not
// transactional, mirroring InsertSpanEvents.
func (d *DB) InsertSpanLinks(links []*SpanLink) error {
	if len(links) == 0 {
		return nil
	}
	return d.namedQuery("storage.InsertSpanLinks").SpanLink.Create(links...)
}

// ListLinksBySpan returns the outbound links emitted by a single span.
func (d *DB) ListLinksBySpan(spanID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListLinksBySpan")
	rows, err := q.SpanLink.ListBySpan(spanID)
	return spanLinkPointers(rows, err)
}

// ListIncomingLinks returns every link in the store whose target is the
// given trace ID — the "who links into this trace?" reverse lookup.
func (d *DB) ListIncomingLinks(linkedTraceID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListIncomingLinks")
	rows, err := q.SpanLink.ListIncomingByTrace(linkedTraceID)
	return spanLinkPointers(rows, err)
}

// ListLinksByTrace returns all span_links whose trace_id matches — used to
// bulk-attach links to spans when serving GET /api/traces/:id so the
// waterfall can show the link badge without a per-span round-trip.
func (d *DB) ListLinksByTrace(traceID string) ([]*SpanLink, error) {
	q := d.namedQuery("storage.ListLinksByTrace")
	rows, err := q.SpanLink.ListByTrace(traceID)
	return spanLinkPointers(rows, err)
}

func spanLinkPointers(rows []model.SpanLink, err error) ([]*SpanLink, error) {
	if err != nil {
		return nil, err
	}
	out := make([]*SpanLink, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

func (d *DB) InsertLog(l *Log) error {
	return d.namedQuery("storage.InsertLog").Log.Create(l)
}

func (d *DB) InsertLintWarning(w *LintWarning) error {
	if err := d.namedQuery("storage.InsertLintWarning").LintWarning.Create(w); err != nil {
		return err
	}
	return d.RecordNotification(&NotificationRecord{
		Source: "lint", SourceID: w.TraceID + ":" + w.SpanID, Severity: w.Severity,
		Title: "Lint: " + w.RuleID, Body: w.Message, Link: "/lint",
		DedupeKey: "lint:" + w.RuleID + ":" + w.SpanID,
	})
}

func (d *DB) RecordNotification(n *NotificationRecord) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt == 0 {
		n.CreatedAt = time.Now().UnixNano()
	}
	if n.DedupeKey != "" {
		existing, err := d.query.NotificationRecord.Where(d.query.NotificationRecord.DedupeKey.Eq(n.DedupeKey)).Order(d.query.NotificationRecord.CreatedAt.Desc()).First()
		if err == nil && n.CreatedAt-existing.CreatedAt < int64(time.Minute) {
			return nil
		}
	}
	return d.namedQuery("storage.RecordNotification").NotificationRecord.Create(n)
}

func (d *DB) ListNotifications(source string, page, limit int) ([]*NotificationRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	q := d.query.NotificationRecord.Where()
	if source != "" {
		q = q.Where(d.query.NotificationRecord.Source.Eq(source))
	}
	total, err := q.Count()
	if err != nil {
		return nil, 0, err
	}
	items, err := q.Order(d.query.NotificationRecord.CreatedAt.Desc()).Offset((page - 1) * limit).Limit(limit).Find()
	return items, total, err
}

func (d *DB) MarkNotificationRead(id string, acknowledged bool) error {
	now := time.Now().UnixNano()
	updates := map[string]any{"read_at": now}
	if acknowledged {
		updates["acknowledged_at"] = now
	}
	_, err := d.query.NotificationRecord.Where(d.query.NotificationRecord.ID.Eq(id)).Updates(updates)
	return err
}

type TraceFilter struct {
	SessionID string
	Service   string
	Limit     int
	Page      int
}

func (d *DB) ListTraces(f TraceFilter) ([]*TraceRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	rows, err := d.query.Span.ListTraces(f.SessionID, f.Service, f.Limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]*TraceRow, len(rows))
	for i, row := range rows {
		result[i] = &TraceRow{
			TraceID: row.TraceID, ServiceName: row.ServiceName, Name: row.Name,
			Attributes: row.Attributes, StatusCode: row.StatusCode, StartNs: row.StartNs,
			EndNs: row.EndNs, DurationNs: row.DurationNs, SessionID: row.SessionID,
			SessionLabel: row.SessionLabel, HasN1: row.HasN1, SpanCount: row.SpanCount,
			IssueKindsRaw: row.IssueKindsRaw,
		}
	}
	for _, row := range result {
		row.IssueKinds = parseKinds(row.IssueKindsRaw)
	}
	return result, nil
}

// CountTraces returns the number of root spans matching a trace-list filter,
// before pagination. A trace is represented by its root span in ListTraces.
func (d *DB) CountTraces(f TraceFilter) (int, error) {
	rows, err := d.query.Span.CountTraces(f.SessionID, f.Service)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rows[0].Count), nil
}

// parseKinds splits a comma-separated list of issue kind strings into a
// deduplicated, sorted slice.
func parseKinds(raw string) []string {
	if raw == "" {
		return []string{}
	}
	seen := map[string]bool{}
	for _, k := range splitTrim(raw, ',') {
		if k != "" {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func splitTrim(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == sep {
			part := strings.TrimSpace(s[start:i])
			out = append(out, part)
			start = i + 1
		}
	}
	return out
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// TraceOverlayFilter scopes the "traces during this window" query used by
// the metrics chart overlay. Service narrows by the metric's service name;
// SessionID by the active session when set. FromNs/ToNs are inclusive.
type TraceOverlayFilter struct {
	Service   string
	SessionID string
	FromNs    int64
	ToNs      int64
	Limit     int
}

// TraceOverlay is the lightweight row returned to the frontend for the
// metrics chart overlay + correlated-traces panel — just enough to draw a
// marker and link out to /traces/:id.
type TraceOverlay = model.TraceOverlay

// ListTracesInWindow returns root spans (traces) whose start_ns falls in
// the [FromNs, ToNs] window for use as chart overlay markers. Caps at
// f.Limit (default 50).
func (d *DB) ListTracesInWindow(f TraceOverlayFilter) ([]*TraceOverlay, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := d.query.Span.ListTraceOverlays(f.Service, f.SessionID, f.FromNs, f.ToNs, f.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]*TraceOverlay, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

func (d *DB) GetTrace(traceID string) ([]*Span, error) {
	return d.query.Span.Where(d.query.Span.TraceID.Eq(traceID)).
		Order(d.query.Span.StartNs).Find()
}

func (d *DB) GetSpan(spanID string) (*Span, error) {
	spans, err := d.query.Span.Where(d.query.Span.SpanID.Eq(spanID)).Limit(1).Find()
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, nil
	}
	return spans[0], nil
}

type SpanFilter struct {
	SessionID string
	Sort      string // "time" | "dur" | "name"
	Limit     int
	Page      int
	Service   string
	Name      string
	Kind      int
	HasKind   bool
}

type SpanRow = model.SpanRow

// ListSpans returns a flat list of spans with computed tag (n+1/slow/lint/error).
// Tags are derived from trace_issues (n+1), status_code (error), duration (slow),
// and lint_warnings (lint). Priority: n+1 > error > slow > lint.
func (d *DB) ListSpans(f SpanFilter) ([]*SpanRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	rows, err := d.query.Span.ListTagged(f.SessionID, f.Service, f.Name, f.HasKind, f.Kind, f.Sort, f.Limit, (f.Page-1)*f.Limit)
	if err != nil {
		return nil, err
	}
	result := make([]*SpanRow, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

// CountSpans returns the full number of spans matching a list filter, before
// pagination. Keeping this separate from ListSpans makes the API metadata
// truthful without making callers load every row.
func (d *DB) CountSpans(f SpanFilter) (int, error) {
	q := d.query.Span.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Span.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Span.ServiceName.Eq(f.Service))
	}
	if f.Name != "" {
		q = q.Where(d.query.Span.Name.Eq(f.Name))
	}
	if f.HasKind {
		q = q.Where(d.query.Span.Kind.Eq(f.Kind))
	}
	count, err := q.Count()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// SpanGroup is an operation-level aggregate. A group deliberately includes
// service and kind: the same name in different services or roles is not the
// same operation.
type SpanGroup = model.SpanGroup

func (d *DB) ListSpanGroups(f SpanFilter) ([]*SpanGroup, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	rows, err := d.query.Span.ListGroups(f.SessionID, f.Limit, (f.Page-1)*f.Limit)
	if err != nil {
		return nil, err
	}
	result := make([]*SpanGroup, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

func (d *DB) CountSpanGroups(f SpanFilter) (int, error) {
	rows, err := d.query.Span.CountGroups(f.SessionID)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rows[0].Count), nil
}

type LogFilter struct {
	SessionID   string
	Service     string
	TraceID     string
	SpanID      string
	MinSeverity int
	MaxSeverity int
	Limit       int
	Page        int
}

func (d *DB) ListLogs(f LogFilter) ([]*Log, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 500
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Log.ServiceName.Eq(f.Service))
	}
	if f.TraceID != "" {
		q = q.Where(d.query.Log.TraceID.Eq(f.TraceID))
	}
	if f.SpanID != "" {
		q = q.Where(d.query.Log.SpanID.Eq(f.SpanID))
	}
	if f.MinSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Gte(f.MinSeverity))
	}
	if f.MaxSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Lte(f.MaxSeverity))
	}
	return q.Order(d.query.Log.TimestampNs.Desc()).Limit(f.Limit).Offset(offset).Find()
}

func (d *DB) CountLogs(f LogFilter) (int, error) {
	q := d.query.Log.Where()
	if f.SessionID != "" {
		q = q.Where(d.query.Log.SessionID.Eq(f.SessionID))
	}
	if f.Service != "" {
		q = q.Where(d.query.Log.ServiceName.Eq(f.Service))
	}
	if f.TraceID != "" {
		q = q.Where(d.query.Log.TraceID.Eq(f.TraceID))
	}
	if f.SpanID != "" {
		q = q.Where(d.query.Log.SpanID.Eq(f.SpanID))
	}
	if f.MinSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Gte(f.MinSeverity))
	}
	if f.MaxSeverity > 0 {
		q = q.Where(d.query.Log.Severity.Lte(f.MaxSeverity))
	}
	count, err := q.Count()
	return int(count), err
}

// ListServices returns distinct service names. An empty sessionID returns
// services across all sessions; a non-empty one scopes to that session.
func (d *DB) ListServices(sessionID string) ([]string, error) {
	var result []string
	q := d.query.Span.Distinct(d.query.Span.ServiceName).Order(d.query.Span.ServiceName)
	if sessionID != "" {
		q = q.Where(d.query.Span.SessionID.Eq(sessionID))
	}
	err := q.Pluck(d.query.Span.ServiceName, &result)
	return result, err
}

func (d *DB) ListSessions() ([]*Session, error) {
	rows, err := d.query.Session.ListWithStats()
	if err != nil {
		return nil, err
	}
	result := make([]*Session, len(rows))
	for i, r := range rows {
		result[i] = &Session{
			ID: r.ID, Label: r.Label, CreatedAt: r.CreatedAt,
			IsBaseline: r.IsBaseline, IsImported: r.IsImported,
			SpanCount: r.SpanCount, Services: r.Services, TraceCount: r.TraceCount,
			Note: r.Note, LastActivityNs: r.LastActivityNs, P95Ns: r.P95Ns,
			SizeBytes: r.SizeBytes, N1Count: r.N1Count, ErrorCount: r.ErrorCount,
		}
	}
	return result, nil
}

func (d *DB) GetSession(id string) (*Session, error) {
	sessions, err := d.query.Session.GetByID(id)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return &sessions[0], nil
}

func (d *DB) SetBaseline(id string, isBaseline bool) error {
	q := d.namedQuery("storage.SetBaseline")
	return q.Transaction(func(tx *querygen.Query) error {
		if isBaseline {
			// clear any previous baseline first
			if _, err := tx.Session.Where(tx.Session.IsBaseline.Is(true)).
				Update(tx.Session.IsBaseline, false); err != nil {
				return err
			}
		}
		_, err := tx.Session.Where(tx.Session.ID.Eq(id)).
			Update(tx.Session.IsBaseline, isBaseline)
		return err
	})
}

// SessionPatch holds the mutable user-facing fields that PATCH /api/sessions/{id} may change.
// A nil pointer means "leave unchanged"; an empty string is a valid value.
type SessionPatch struct {
	Label *string
	Note  *string
}

func (d *DB) UpdateSession(id string, p SessionPatch) error {
	updates := map[string]any{}
	if p.Label != nil {
		updates["label"] = *p.Label
	}
	if p.Note != nil {
		updates["note"] = *p.Note
	}
	if len(updates) == 0 {
		return nil
	}
	q := d.namedQuery("storage.UpdateSession")
	_, err := q.Session.Where(q.Session.ID.Eq(id)).Updates(updates)
	return err
}

func (d *DB) DeleteSession(id string) error {
	q := d.namedQuery("storage.DeleteSession")
	return q.Transaction(func(tx *querygen.Query) error {
		if _, err := tx.LintWarning.Where(tx.LintWarning.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.TraceIssue.Where(tx.TraceIssue.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Log.Where(tx.Log.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Metric.Where(tx.Metric.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanEvent.Where(tx.SpanEvent.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanLink.Where(tx.SpanLink.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.Span.Where(tx.Span.SessionID.Eq(id)).Delete(); err != nil {
			return err
		}
		_, err := tx.Session.Where(tx.Session.ID.Eq(id)).Delete()
		return err
	})
}

// DeleteSpansNamed removes every span with name and the rows that directly
// reference those spans. It is intended for narrow telemetry repair actions;
// it deliberately leaves all other spans in the affected traces intact.
func (d *DB) DeleteSpansNamed(name string) (int, error) {
	q := d.namedQuery("storage.DeleteSpansNamed")
	spans, err := q.Span.Where(q.Span.Name.Eq(name)).Find()
	if err != nil {
		return 0, err
	}
	if len(spans) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(spans))
	for _, span := range spans {
		ids = append(ids, span.SpanID)
	}
	err = q.Transaction(func(tx *querygen.Query) error {
		if _, err := tx.SpanEvent.Where(tx.SpanEvent.SpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		if _, err := tx.SpanLink.Where(tx.SpanLink.SpanID.In(ids...)).Or(tx.SpanLink.LinkedSpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		if _, err := tx.LintWarning.Where(tx.LintWarning.SpanID.In(ids...)).Delete(); err != nil {
			return err
		}
		_, err := tx.Span.Where(tx.Span.SpanID.In(ids...)).Delete()
		return err
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (d *DB) ListLintWarnings(sessionID string) ([]*LintWarning, error) {
	rows, err := d.query.LintWarning.ListWithTraceIssues(sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]*LintWarning, len(rows))
	for i := range rows {
		result[i] = &rows[i]
	}
	return result, nil
}

func (d *DB) GetStats(sessionID string) (*Stats, error) {
	s := &Stats{StorageFull: d.Full()}

	// Every complete OpenTelemetry trace has exactly one root span. Counting
	// roots avoids an exact COUNT(DISTINCT trace_id), whose hash table grew to
	// multiple GiB on a modest on-disk store and stalled the stats endpoint.
	// This named query combines all dashboard aggregates into one round trip.
	rows, err := d.namedQuery("storage.GetStats").Span.GetStats(sessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		s.SpanCount = int(rows[0].SpanCount)
		s.TraceCount = int(rows[0].TraceCount)
		s.LogCount = int(rows[0].LogCount)
		s.SessionCount = int(rows[0].SessionCount)
		s.OldestSessionAt = rows[0].OldestSessionAt
	}

	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			s.DBSize = fi.Size()
		}
	}
	return s, nil
}

// GetSourceStats returns per-service ingest stats for the given session
// (or all sessions when sessionID is ""). Rates are derived from received_at.
func (d *DB) GetSourceStats(sessionID string) ([]SourceStats, error) {
	rows, err := d.query.Span.ListSourceStats(sessionID)
	if err != nil {
		return nil, err
	}

	out := make([]SourceStats, 0, len(rows))
	for _, r := range rows {
		durSec := float64(r.LastSeen-r.FirstSeen) / 1e9
		if durSec < 1 {
			durSec = 1
		}
		var errRate float64
		if r.SpanCount > 0 {
			errRate = float64(r.ErrorCount) / float64(r.SpanCount)
		}
		out = append(out, SourceStats{
			Service:        r.ServiceName,
			AcceptedPerSec: float64(r.SpanCount) / durSec,
			RejectedPerSec: 0,
			ErrorRate:      errRate,
			BytesPerSec:    float64(r.BytesTotal) / durSec,
			LastSeenNs:     r.LastSeen,
		})
	}
	return out, nil
}

func (d *DB) GetServiceMap(sessionID string) (*ServiceMapData, error) {
	nodeRows, err := d.query.Span.ListServiceMapNodes(sessionID)
	if err != nil {
		return nil, fmt.Errorf("service map nodes: %w", err)
	}
	nodes := make([]*ServiceMapNode, len(nodeRows))
	for i := range nodeRows {
		nodes[i] = &nodeRows[i]
	}

	// Top operations per service for the inspector panel.
	for _, n := range nodes {
		ops, err := d.topOperationsForService(n.ID, sessionID, 5)
		if err != nil {
			return nil, fmt.Errorf("top ops for %s: %w", n.ID, err)
		}
		n.TopOps = ops
	}

	edgeRows, err := d.query.Span.ListServiceMapEdges(sessionID)
	if err != nil {
		return nil, fmt.Errorf("service map edges: %w", err)
	}
	edges := make([]*ServiceMapEdge, len(edgeRows))
	for i := range edgeRows {
		edges[i] = &edgeRows[i]
	}

	if nodes == nil {
		nodes = []*ServiceMapNode{}
	}
	if edges == nil {
		edges = []*ServiceMapEdge{}
	}
	return &ServiceMapData{Nodes: nodes, Edges: edges}, nil
}

// topOperationsForService returns the N most-common (service, name) pairs for
// the inspector panel, with per-op p95 latency.
func (d *DB) topOperationsForService(service, sessionID string, limit int) ([]ServiceMapOpStat, error) {
	return d.query.Span.ListTopOperations(service, sessionID, limit)
}

// LoadDropCounters reads the persisted drop counters from the meta table.
// Returns zeros (no error) if not yet written.
func (d *DB) LoadDropCounters() (spans, logs, metrics int64, err error) {
	rows, err := d.query.Meta.Where(d.query.Meta.Key.In(
		"dropped_spans", "dropped_logs", "dropped_metric_points",
	)).Find()
	if err != nil {
		return
	}
	for _, r := range rows {
		val, parseErr := strconv.ParseInt(r.Value, 10, 64)
		if parseErr != nil {
			continue
		}
		switch r.Key {
		case "dropped_spans":
			spans = val
		case "dropped_logs":
			logs = val
		case "dropped_metric_points":
			metrics = val
		}
	}
	return
}

// SaveDropCounters writes the current drop counters to the meta table.
func (d *DB) SaveDropCounters(spans, logs, metrics int64) error {
	q := d.namedQuery("storage.SaveDropCounters")
	return q.Transaction(func(tx *querygen.Query) error {
		for _, entry := range []*model.Meta{
			{Key: "dropped_spans", Value: strconv.FormatInt(spans, 10)},
			{Key: "dropped_logs", Value: strconv.FormatInt(logs, 10)},
			{Key: "dropped_metric_points", Value: strconv.FormatInt(metrics, 10)},
		} {
			if err := tx.Meta.UpsertValue(entry.Key, entry.Value); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetServiceP95 returns the p95 duration_ns for spans of the given service.
// Returns 0 (no error) when there are no stored spans for that service yet.
func (d *DB) GetServiceP95(serviceName string) (int64, error) {
	rows, err := d.query.Span.ServiceP95(serviceName)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].P95Ns, nil
}

func (d *DB) UpsertTraceIssue(issue *TraceIssue) error {
	return d.namedQuery("storage.UpsertTraceIssue").TraceIssue.Save(issue)
}

func (d *DB) GetTraceIssues(traceID string) ([]*TraceIssue, error) {
	result, err := d.query.TraceIssue.Where(d.query.TraceIssue.TraceID.Eq(traceID)).
		Order(d.query.TraceIssue.WastedNs.Desc()).Find()
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []*TraceIssue{}
	}
	return result, nil
}

// GetSpansBySession returns all spans for a session, ordered by start time.
func (d *DB) GetSpansBySession(sessionID string) ([]*Span, error) {
	return d.query.Span.Where(d.query.Span.SessionID.Eq(sessionID)).
		Order(d.query.Span.StartNs).Find()
}

// ListCoverageOperations returns one row per observed operation in a session.
// Route extraction and percentile aggregation stay in DuckDB so coverage does
// not need to materialize the complete session in memory.
func (d *DB) ListCoverageOperations(sessionID string) ([]*CoverageOperation, error) {
	rows, err := d.namedQuery("storage.ListCoverageOperations").Span.ListCoverageOperations(sessionID)
	if err != nil {
		return nil, err
	}
	operations := make([]*CoverageOperation, len(rows))
	for i := range rows {
		operations[i] = &rows[i]
	}
	return operations, nil
}

func (d *DB) GetCoverageQuality(sessionID string) (*CoverageQuality, error) {
	rows, err := d.namedQuery("storage.GetCoverageQuality").Span.GetCoverageQuality(sessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &CoverageQuality{}, nil
	}
	return &rows[0], nil
}

// InsertMetric stores one metric data point.
func (d *DB) InsertMetric(m *Metric) error {
	return d.namedQuery("storage.InsertMetric").Metric.Create(m)
}

// ActiveMetricSeries returns the durable source for the observable
// active-series gauge without reconstructing identities from raw points.
func (d *DB) ActiveMetricSeries() (int64, error) {
	return d.namedQuery("storage.ActiveMetricSeries").MetricSeriesCatalog.Count()
}

// ListMetricCatalog returns one entry per (service, name) seen in the session.
// Pass "" to ignore the session filter.
func (d *DB) ListMetricCatalog(sessionID string) ([]*MetricCatalogEntry, error) {
	rows, err := d.query.Metric.ListCatalog(sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*MetricCatalogEntry, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

// GetMetricStreamMetadata returns the stable catalog fields for a stream,
// without applying a time window.
func (d *DB) GetMetricStreamMetadata(f MetricSeriesFilter) (*Metric, error) {
	rows, err := d.query.Metric.GetStreamMetadata(f.Name, f.Service, f.SessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// MetricSeriesFilter scopes a series query.
type MetricSeriesFilter struct {
	Name      string
	Service   string
	SessionID string
	FromNs    int64 // inclusive; 0 = no lower bound
	ToNs      int64 // inclusive; 0 = no upper bound
}

// GetMetricSeries returns every data point matching the filter, ordered by
// timestamp ascending. Histogram percentiles arrive as separate rows; callers
// split them apart by attributes.percentile.
func (d *DB) GetMetricSeries(f MetricSeriesFilter) ([]*Metric, error) {
	q := d.query.Metric.Where()
	if f.Name != "" {
		q = q.Where(d.query.Metric.Name.Eq(f.Name))
	}
	if f.Service != "" {
		q = q.Where(d.query.Metric.ServiceName.Eq(f.Service))
	}
	if f.SessionID != "" {
		q = q.Where(d.query.Metric.SessionID.Eq(f.SessionID))
	}
	if f.FromNs > 0 {
		q = q.Where(d.query.Metric.TimestampNs.Gte(f.FromNs))
	}
	if f.ToNs > 0 {
		q = q.Where(d.query.Metric.TimestampNs.Lte(f.ToNs))
	}
	return q.Order(d.query.Metric.TimestampNs).Find()
}

// ListTraceIssuesBySession returns every detector finding for a session.
func (d *DB) ListTraceIssuesBySession(sessionID string) ([]*TraceIssue, error) {
	return d.query.TraceIssue.Where(d.query.TraceIssue.SessionID.Eq(sessionID)).
		Order(d.query.TraceIssue.WastedNs.Desc()).Find()
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
// per-session estimate based on the serialised span attribute lengths.
func (d *DB) GetStorageBreakdown() (*StorageBreakdown, error) {
	out := &StorageBreakdown{}

	// DuckDB does not expose per-table compressed sizes in this version.
	// Checkpoint first to flush the WAL, then distribute the actual on-disk
	// file size proportionally using uncompressed payload lengths as weights.
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

	// Sum total payload to use as denominator for proportioning.
	var totalPayload int64
	for _, s := range sizes {
		totalPayload += s.PayloadBytes
	}

	byTable := make(map[string]model.TableSizeRow, len(sizes))
	for _, s := range sizes {
		byTable[s.Name] = s
	}
	for _, name := range tableNames {
		s := byTable[name]
		approxBytes := s.PayloadBytes
		// If we have a real file size, scale proportionally so values reflect
		// actual on-disk bytes rather than uncompressed payload lengths.
		if fileBytes > 0 && totalPayload > 0 {
			approxBytes = fileBytes * s.PayloadBytes / totalPayload
		}
		out.Tables = append(out.Tables, TableStat{
			Name:        name,
			RowCount:    s.RowCount,
			ApproxBytes: approxBytes,
		})
	}

	// Per-session: proxy size via span attribute payload length.
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

// CompactResult reports bytes before and after a CHECKPOINT + VACUUM cycle.
type CompactResult struct {
	BytesBefore int64 `json:"bytes_before"`
	BytesAfter  int64 `json:"bytes_after"`
	Reclaimed   int64 `json:"reclaimed"`
}

// Compact runs CHECKPOINT followed by VACUUM to return free pages to the OS.
func (d *DB) Compact() (*CompactResult, error) {
	res := &CompactResult{}
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			res.BytesBefore = fi.Size()
		}
	}
	if err := d.withMaintenance(func() error {
		if err := d.checkpointWithRetry(); err != nil {
			return fmt.Errorf("checkpoint: %w", err)
		}
		if err := d.gorm.Exec("VACUUM").Error; err != nil {
			return fmt.Errorf("vacuum: %w", err)
		}
		return d.checkpointWithRetry()
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
	return d.withMaintenance(func() error {
		return d.checkpointWithRetry()
	})
}

func (d *DB) withMaintenance(fn func() error) error {
	telemetry.Catalog().SetStorageMaintenanceInFlight(1)
	defer telemetry.Catalog().SetStorageMaintenanceInFlight(0)
	if d.batcher == nil {
		return fn()
	}
	return d.batcher.Maintenance(fn)
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
