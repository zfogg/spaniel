package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/alifiroozi80/duckdb"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type DB struct {
	gorm               *gorm.DB
	path               string
	activeSessionID    string
	activeSessionLabel string
}

type Span struct {
	TraceID       string       `json:"trace_id"`
	SpanID        string       `json:"span_id"`
	ParentSpanID  string       `json:"parent_span_id"`
	ServiceName   string       `json:"service_name"`
	Name          string       `json:"name"`
	Kind          int          `json:"kind"`
	StartNs       int64        `json:"start_ns"`
	EndNs         int64        `json:"end_ns"`
	DurationNs    int64        `json:"duration_ns" gorm:"->"` // generated column, read-only
	StatusCode    int          `json:"status_code"`
	StatusMessage string       `json:"status_message"`
	Attributes    string       `json:"attributes"`
	Resource      string       `json:"resource"`
	SessionID     string       `json:"session_id"`
	SessionLabel  string       `json:"session_label"`
	ReceivedAt    int64        `json:"received_at"`
	Events        []*SpanEvent `json:"events" gorm:"-"`
	Links         []*SpanLink  `json:"links" gorm:"-"`
}

func (Span) TableName() string { return "spans" }

type Log struct {
	TimestampNs int64  `json:"timestamp_ns"`
	TraceID     string `json:"trace_id"`
	SpanID      string `json:"span_id"`
	Severity    int    `json:"severity"`
	Body        string `json:"body"`
	Attributes  string `json:"attributes"`
	ServiceName string `json:"service_name"`
	SessionID   string `json:"session_id"`
	ReceivedAt  int64  `json:"received_at"`
}

func (Log) TableName() string { return "logs" }

type Session struct {
	ID         string `json:"id" gorm:"primaryKey"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	IsBaseline bool   `json:"is_baseline"`
	IsImported bool   `json:"is_imported"`
	SpanCount  int    `json:"span_count"`
	TraceCount int    `json:"trace_count" gorm:"-"` // computed via join, not a column
	Services   string `json:"services"`
}

func (Session) TableName() string { return "sessions" }

type LintWarning struct {
	SpanID    string `json:"span_id"`
	TraceID   string `json:"trace_id"`
	SessionID string `json:"session_id"`
	RuleID    string `json:"rule_id"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	CreatedAt int64  `json:"created_at"`
}

func (LintWarning) TableName() string { return "lint_warnings" }

type TraceRow struct {
	TraceID      string `json:"trace_id"`
	ServiceName  string `json:"service_name"`
	Name         string `json:"name"`
	StatusCode   int    `json:"status_code"`
	StartNs      int64  `json:"start_ns"`
	EndNs        int64  `json:"end_ns"`
	DurationNs   int64  `json:"duration_ns"`
	SessionID    string `json:"session_id"`
	SessionLabel string `json:"session_label"`
	HasN1        bool   `json:"has_n1"`
	SpanCount    int    `json:"span_count"`
}

type TraceIssue struct {
	ID            string `json:"id" gorm:"primaryKey"`
	TraceID       string `json:"trace_id"`
	SessionID     string `json:"session_id"`
	Kind          string `json:"kind"`
	Fingerprint   string `json:"fingerprint"`
	Count         int    `json:"count"`
	WastedNs      int64  `json:"wasted_ns"`
	ParentSpanID  string `json:"parent_span_id"`
	ExampleSpanID string `json:"example_span_id"`
	CreatedAt     int64  `json:"created_at"`
}

func (TraceIssue) TableName() string { return "trace_issues" }

type SpanEvent struct {
	SpanID     string `json:"span_id"`
	TraceID    string `json:"trace_id"`
	SessionID  string `json:"session_id"`
	TimeNs     int64  `json:"time_ns"`
	Name       string `json:"name"`
	Attributes string `json:"attributes"`
}

func (SpanEvent) TableName() string { return "span_events" }

// SpanLink is a causal/relational pointer from one span to another span
// (potentially in a different trace). OTel uses these for fan-out work
// items, batched jobs, async retries — anywhere a span was caused by
// something not on its direct parent chain.
type SpanLink struct {
	SpanID        string `json:"span_id"`
	TraceID       string `json:"trace_id"`
	SessionID     string `json:"session_id"`
	LinkedTraceID string `json:"linked_trace_id"`
	LinkedSpanID  string `json:"linked_span_id"`
	TraceState    string `json:"trace_state"`
	Attributes    string `json:"attributes"`
}

func (SpanLink) TableName() string { return "span_links" }

type Stats struct {
	SpanCount       int   `json:"span_count"`
	TraceCount      int   `json:"trace_count"`
	LogCount        int   `json:"log_count"`
	DBSize          int64 `json:"db_size"`
	SessionCount    int   `json:"session_count"`
	OldestSessionAt int64 `json:"oldest_session_at"`
}

// Metric is one data point of an OTLP metric (gauge, counter, or one
// percentile of a histogram). Histogram data points are stored as three rows
// (p50/p95/p99) with the percentile encoded in attributes.percentile.
type Metric struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	Type        string  `json:"type"` // gauge | counter | histogram
	TimestampNs int64   `json:"timestamp_ns"`
	Value       float64 `json:"value"`
	Attributes  string  `json:"attributes"`
	ServiceName string  `json:"service_name"`
	SessionID   string  `json:"session_id"`
}

func (Metric) TableName() string { return "metrics" }

// MetricCatalogEntry summarizes one (name, service) metric stream.
type MetricCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Type        string `json:"type"`
	ServiceName string `json:"service_name"`
	SampleCount int    `json:"sample_count"`
}

type ServiceMapNode struct {
	ID         string             `json:"id"`
	SpanCount  int                `json:"span_count"`
	ErrorCount int                `json:"error_count"`
	P95Ns      int64              `json:"p95_ns"`
	TopOps     []ServiceMapOpStat `json:"top_operations" gorm:"-"`
}

// ServiceMapOpStat is one (operation name, count, p95) entry for the node
// inspector panel. Capped server-side to keep the response cheap.
type ServiceMapOpStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	P95Ns int64  `json:"p95_ns"`
}

type ServiceMapEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	CallCount     int    `json:"call_count"`
	AvgDurationNs int64  `json:"avg_duration_ns"`
	ErrorCount    int    `json:"error_count"`
}

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

func Open(path string) (*DB, error) {
	g, err := gorm.Open(duckdb.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, fmt.Errorf("duckdb pool: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	d := &DB{gorm: g, path: path}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
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
	if err := d.gorm.Create(s).Error; err != nil {
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

// Gorm exposes the underlying *gorm.DB for callers that want to build queries.
func (d *DB) Gorm() *gorm.DB { return d.gorm }

func (d *DB) ActiveSessionID() string    { return d.activeSessionID }
func (d *DB) ActiveSessionLabel() string { return d.activeSessionLabel }

func (d *DB) InsertSpan(s *Span) error {
	// duration_ns is a generated column (gorm:"->") — never written.
	return d.gorm.Create(s).Error
}

// InsertSpanEvents bulk-inserts the events attached to a span. Empty input
// is a no-op; on error any rows inserted before the failure are not rolled
// back (events are best-effort, not transactional, like spans).
func (d *DB) InsertSpanEvents(events []*SpanEvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.gorm.Create(&events).Error
}

// ListEventsBySpan returns the events attached to a single span, in time order.
func (d *DB) ListEventsBySpan(spanID string) ([]*SpanEvent, error) {
	var out []*SpanEvent
	err := d.gorm.Table("span_events").
		Select(`span_id, trace_id, session_id, time_ns, name, attributes::VARCHAR AS attributes`).
		Where("span_id = ?", spanID).
		Order("time_ns ASC").
		Find(&out).Error
	return out, err
}

// InsertSpanLinks bulk-inserts links emitted by a span. Best-effort: not
// transactional, mirroring InsertSpanEvents.
func (d *DB) InsertSpanLinks(links []*SpanLink) error {
	if len(links) == 0 {
		return nil
	}
	return d.gorm.Create(&links).Error
}

const linkCols = `span_id, trace_id, session_id, linked_trace_id, linked_span_id, trace_state, attributes::VARCHAR AS attributes`

// ListLinksBySpan returns the outbound links emitted by a single span.
func (d *DB) ListLinksBySpan(spanID string) ([]*SpanLink, error) {
	var out []*SpanLink
	err := d.gorm.Table("span_links").Select(linkCols).
		Where("span_id = ?", spanID).Find(&out).Error
	return out, err
}

// ListIncomingLinks returns every link in the store whose target is the
// given trace ID — the "who links into this trace?" reverse lookup.
func (d *DB) ListIncomingLinks(linkedTraceID string) ([]*SpanLink, error) {
	var out []*SpanLink
	err := d.gorm.Table("span_links").Select(linkCols).
		Where("linked_trace_id = ?", linkedTraceID).Find(&out).Error
	return out, err
}

// ListLinksByTrace returns all span_links whose trace_id matches — used to
// bulk-attach links to spans when serving GET /api/traces/:id so the
// waterfall can show the link badge without a per-span round-trip.
func (d *DB) ListLinksByTrace(traceID string) ([]*SpanLink, error) {
	var out []*SpanLink
	err := d.gorm.Table("span_links").Select(linkCols).
		Where("trace_id = ?", traceID).Find(&out).Error
	return out, err
}

func (d *DB) InsertLog(l *Log) error {
	return d.gorm.Create(l).Error
}

func (d *DB) InsertLintWarning(w *LintWarning) error {
	return d.gorm.Create(w).Error
}

type TraceFilter struct {
	SessionID string
	Service   string
	Limit     int
	Page      int
}

func (d *DB) ListTraces(f TraceFilter) ([]*TraceRow, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	query := `
		SELECT s.trace_id, s.service_name, s.name, s.status_code, s.start_ns, s.end_ns, s.duration_ns, s.session_id, s.session_label,
		       COALESCE(ti.has_n1, FALSE) AS has_n1,
		       COALESCE(sc.span_count, 1) AS span_count
		FROM spans s
		LEFT JOIN (
			SELECT trace_id, TRUE AS has_n1
			FROM trace_issues WHERE kind = 'n_plus_one'
			GROUP BY trace_id
		) ti ON s.trace_id = ti.trace_id
		LEFT JOIN (
			SELECT trace_id, COUNT(*) AS span_count FROM spans GROUP BY trace_id
		) sc ON s.trace_id = sc.trace_id
		WHERE (s.parent_span_id = '' OR s.parent_span_id IS NULL)`
	args := []any{}

	if f.SessionID != "" {
		query += ` AND s.session_id = ?`
		args = append(args, f.SessionID)
	}
	if f.Service != "" {
		query += ` AND s.service_name = ?`
		args = append(args, f.Service)
	}
	query += ` ORDER BY s.start_ns DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, offset)

	var result []*TraceRow
	if err := d.gorm.Raw(query, args...).Scan(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
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
type TraceOverlay struct {
	TraceID    string `json:"trace_id"`
	Op         string `json:"op"`
	Service    string `json:"service"`
	StatusCode int    `json:"status_code"`
	StartNs    int64  `json:"start_ns"`
	EndNs      int64  `json:"end_ns"`
	DurationNs int64  `json:"duration_ns"`
}

// ListTracesInWindow returns root spans (traces) whose start_ns falls in
// the [FromNs, ToNs] window for use as chart overlay markers. Caps at
// f.Limit (default 50).
func (d *DB) ListTracesInWindow(f TraceOverlayFilter) ([]*TraceOverlay, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q := d.gorm.Table("spans").
		Select(`trace_id, name AS op, service_name AS service, status_code, start_ns, end_ns, duration_ns`).
		Where(`parent_span_id = '' OR parent_span_id IS NULL`)
	if f.Service != "" {
		q = q.Where("service_name = ?", f.Service)
	}
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.FromNs > 0 {
		q = q.Where("start_ns >= ?", f.FromNs)
	}
	if f.ToNs > 0 {
		q = q.Where("start_ns <= ?", f.ToNs)
	}
	var out []*TraceOverlay
	err := q.Order("start_ns ASC").Limit(f.Limit).Scan(&out).Error
	return out, err
}

func (d *DB) GetTrace(traceID string) ([]*Span, error) {
	var result []*Span
	err := d.gorm.Table("spans").Select(spanCols).
		Where("trace_id = ?", traceID).Order("start_ns").Find(&result).Error
	return result, err
}

func (d *DB) GetSpan(spanID string) (*Span, error) {
	var spans []*Span
	err := d.gorm.Table("spans").Select(spanCols).
		Where("span_id = ?", spanID).Limit(1).Find(&spans).Error
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
}

type SpanRow struct {
	Span
	Tag string `json:"tag,omitempty"`
}

// ListSpans returns a flat list of spans with computed tag (n+1/slow/lint/error).
// Tags are derived from trace_issues (n+1), status_code (error), duration (slow),
// and lint_warnings (lint). Priority: n+1 > error > slow > lint.
func (d *DB) ListSpans(f SpanFilter) ([]*SpanRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 500
	}
	orderBy := "s.start_ns DESC"
	switch f.Sort {
	case "dur":
		orderBy = "s.duration_ns DESC"
	case "name":
		orderBy = "s.name ASC"
	}
	//nolint:gosec // orderBy is constrained to safe values above
	query := `
		SELECT
			s.trace_id, s.span_id, s.parent_span_id, s.service_name, s.name, s.kind,
			s.start_ns, s.end_ns, s.duration_ns, s.status_code, s.status_message,
			s.attributes::VARCHAR AS attributes, s.resource::VARCHAR AS resource, s.session_id, s.session_label, s.received_at,
			CASE
				WHEN ni.trace_id IS NOT NULL  THEN 'n+1'
				WHEN s.status_code = 2        THEN 'error'
				WHEN s.duration_ns > 250000000 THEN 'slow'
				WHEN lw.span_id IS NOT NULL   THEN 'lint'
				ELSE ''
			END AS tag
		FROM spans s
		LEFT JOIN (SELECT DISTINCT trace_id FROM trace_issues WHERE kind = 'n_plus_one') ni
			ON s.trace_id = ni.trace_id
		LEFT JOIN (SELECT DISTINCT span_id FROM lint_warnings) lw
			ON s.span_id = lw.span_id
		WHERE (? = '' OR s.session_id = ?)
		ORDER BY ` + orderBy + `
		LIMIT ?`
	var result []*SpanRow
	if err := d.gorm.Raw(query, f.SessionID, f.SessionID, f.Limit).Scan(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

type LogFilter struct {
	SessionID string
	TraceID   string
	SpanID    string
	Limit     int
	Page      int
}

func (d *DB) ListLogs(f LogFilter) ([]*Log, error) {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	q := d.gorm.Table("logs").
		Select(`timestamp_ns, trace_id, span_id, severity, body, attributes::VARCHAR AS attributes, service_name, session_id, received_at`)
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.TraceID != "" {
		q = q.Where("trace_id = ?", f.TraceID)
	}
	if f.SpanID != "" {
		q = q.Where("span_id = ?", f.SpanID)
	}
	var result []*Log
	err := q.Order("timestamp_ns DESC").Limit(f.Limit).Offset(offset).Find(&result).Error
	return result, err
}

func (d *DB) ListServices() ([]string, error) {
	var result []string
	err := d.gorm.Table("spans").
		Distinct("service_name").Order("service_name").
		Pluck("service_name", &result).Error
	return result, err
}

func (d *DB) ListSessions() ([]*Session, error) {
	// Joins spans to count distinct traces per session — not expressible as a
	// plain Find because trace_count is a computed aggregate.
	type sessionRow struct {
		ID         string
		Label      string
		CreatedAt  int64
		IsBaseline bool
		IsImported bool
		SpanCount  int
		Services   string
		TraceCount int
	}
	var rows []sessionRow
	err := d.gorm.Raw(`
		SELECT s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services::VARCHAR AS services,
		       COUNT(DISTINCT sp.trace_id) AS trace_count
		FROM sessions s
		LEFT JOIN spans sp ON sp.session_id = s.id
		GROUP BY s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services
		ORDER BY s.created_at DESC`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]*Session, 0, len(rows))
	for _, r := range rows {
		result = append(result, &Session{
			ID: r.ID, Label: r.Label, CreatedAt: r.CreatedAt,
			IsBaseline: r.IsBaseline, IsImported: r.IsImported,
			SpanCount: r.SpanCount, Services: r.Services, TraceCount: r.TraceCount,
		})
	}
	return result, nil
}

func (d *DB) GetSession(id string) (*Session, error) {
	var sessions []*Session
	err := d.gorm.Table("sessions").
		Select(`id, label, created_at, is_baseline, is_imported, span_count, services::VARCHAR AS services`).
		Where("id = ?", id).Limit(1).Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return sessions[0], nil
}

func (d *DB) SetBaseline(id string, isBaseline bool) error {
	if isBaseline {
		// clear any previous baseline first
		if err := d.gorm.Model(&Session{}).Where("is_baseline = ?", true).
			Update("is_baseline", false).Error; err != nil {
			return err
		}
	}
	return d.gorm.Model(&Session{}).Where("id = ?", id).
		Update("is_baseline", isBaseline).Error
}

func (d *DB) DeleteSession(id string) error {
	for _, tbl := range []string{"lint_warnings", "trace_issues", "logs", "metrics", "span_events", "span_links", "spans"} {
		if err := d.gorm.Exec(`DELETE FROM `+tbl+` WHERE session_id = ?`, id).Error; err != nil {
			return err
		}
	}
	return d.gorm.Exec(`DELETE FROM sessions WHERE id = ?`, id).Error
}

func (d *DB) ListLintWarnings(sessionID string) ([]*LintWarning, error) {
	q := d.gorm.Table("lint_warnings").
		Select(`span_id, trace_id, session_id, rule_id, message, severity, created_at`)
	if sessionID != "" {
		q = q.Where("session_id = ?", sessionID).Order("created_at DESC")
	} else {
		q = q.Order("created_at DESC").Limit(500)
	}
	var result []*LintWarning
	err := q.Find(&result).Error
	return result, err
}

func (d *DB) GetStats(sessionID string) (*Stats, error) {
	s := &Stats{}

	spanQ := d.gorm.Table("spans")
	traceQ := d.gorm.Table("spans").Distinct("trace_id")
	logQ := d.gorm.Table("logs")
	if sessionID != "" {
		spanQ = spanQ.Where("session_id = ?", sessionID)
		traceQ = traceQ.Where("session_id = ?", sessionID)
		logQ = logQ.Where("session_id = ?", sessionID)
	}

	var spanCount, traceCount, logCount int64
	if err := spanQ.Count(&spanCount).Error; err != nil {
		return nil, err
	}
	if err := traceQ.Count(&traceCount).Error; err != nil {
		return nil, err
	}
	if err := logQ.Count(&logCount).Error; err != nil {
		return nil, err
	}
	s.SpanCount = int(spanCount)
	s.TraceCount = int(traceCount)
	s.LogCount = int(logCount)

	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			s.DBSize = fi.Size()
		}
	}
	var sessionCount int64
	_ = d.gorm.Table("sessions").Count(&sessionCount).Error
	s.SessionCount = int(sessionCount)
	var oldest sql.NullInt64
	_ = d.gorm.Raw(`SELECT MIN(created_at) FROM sessions`).Scan(&oldest).Error
	if oldest.Valid {
		s.OldestSessionAt = oldest.Int64
	}
	return s, nil
}

func (d *DB) GetServiceMap(sessionID string) (*ServiceMapData, error) {
	// Nodes — span_count, error_count, p95(duration_ns).
	nodeQuery := `
		SELECT service_name AS id,
		       COUNT(*) AS span_count,
		       COUNT(*) FILTER (WHERE status_code = 2) AS error_count,
		       CAST(COALESCE(QUANTILE_CONT(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns
		FROM spans`
	nodeArgs := []any{}
	if sessionID != "" {
		nodeQuery += ` WHERE session_id = ?`
		nodeArgs = append(nodeArgs, sessionID)
	}
	nodeQuery += ` GROUP BY service_name ORDER BY service_name`

	var nodes []*ServiceMapNode
	if err := d.gorm.Raw(nodeQuery, nodeArgs...).Scan(&nodes).Error; err != nil {
		return nil, fmt.Errorf("service map nodes: %w", err)
	}

	// Top operations per service for the inspector panel.
	for _, n := range nodes {
		ops, err := d.topOperationsForService(n.ID, sessionID, 5)
		if err != nil {
			return nil, fmt.Errorf("top ops for %s: %w", n.ID, err)
		}
		n.TopOps = ops
	}

	// Edges — parent → child where services differ. Adds error_count: the
	// count of child spans on the edge that errored (status_code = 2).
	edgeQuery := `
		SELECT p.service_name AS "from",
		       c.service_name AS "to",
		       COUNT(*) AS call_count,
		       CAST(AVG(c.duration_ns) AS BIGINT) AS avg_duration_ns,
		       COUNT(*) FILTER (WHERE c.status_code = 2) AS error_count
		FROM spans c
		INNER JOIN spans p ON c.parent_span_id = p.span_id
		WHERE c.service_name != p.service_name`
	edgeArgs := []any{}
	if sessionID != "" {
		edgeQuery += ` AND c.session_id = ?`
		edgeArgs = append(edgeArgs, sessionID)
	}
	edgeQuery += ` GROUP BY p.service_name, c.service_name`

	var edges []*ServiceMapEdge
	if err := d.gorm.Raw(edgeQuery, edgeArgs...).Scan(&edges).Error; err != nil {
		return nil, fmt.Errorf("service map edges: %w", err)
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
	q := `
		SELECT name,
		       COUNT(*) AS count,
		       CAST(COALESCE(QUANTILE_CONT(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns
		FROM spans
		WHERE service_name = ?`
	args := []any{service}
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	q += ` GROUP BY name ORDER BY count DESC LIMIT ?`
	args = append(args, limit)

	out := []ServiceMapOpStat{}
	if err := d.gorm.Raw(q, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DB) UpsertTraceIssue(issue *TraceIssue) error {
	return d.gorm.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(issue).Error
}

func (d *DB) GetTraceIssues(traceID string) ([]*TraceIssue, error) {
	var result []*TraceIssue
	err := d.gorm.Where("trace_id = ?", traceID).Order("wasted_ns DESC").Find(&result).Error
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
	var result []*Span
	err := d.gorm.Table("spans").Select(spanCols).
		Where("session_id = ?", sessionID).Order("start_ns ASC").Find(&result).Error
	return result, err
}

// InsertMetric stores one metric data point.
func (d *DB) InsertMetric(m *Metric) error {
	return d.gorm.Create(m).Error
}

// ListMetricCatalog returns one entry per (service, name) seen in the session.
// Pass "" to ignore the session filter.
func (d *DB) ListMetricCatalog(sessionID string) ([]*MetricCatalogEntry, error) {
	q := `
		SELECT name, service_name, type, unit, description, COUNT(*) AS sample_count
		FROM metrics`
	args := []any{}
	if sessionID != "" {
		q += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	q += ` GROUP BY name, service_name, type, unit, description
	       ORDER BY service_name, name`
	var out []*MetricCatalogEntry
	err := d.gorm.Raw(q, args...).Scan(&out).Error
	return out, err
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
	q := d.gorm.Table("metrics").
		Select(`name, description, unit, type, timestamp_ns, value, attributes::VARCHAR AS attributes, service_name, session_id`)
	if f.Name != "" {
		q = q.Where("name = ?", f.Name)
	}
	if f.Service != "" {
		q = q.Where("service_name = ?", f.Service)
	}
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.FromNs > 0 {
		q = q.Where("timestamp_ns >= ?", f.FromNs)
	}
	if f.ToNs > 0 {
		q = q.Where("timestamp_ns <= ?", f.ToNs)
	}
	var out []*Metric
	err := q.Order("timestamp_ns ASC").Find(&out).Error
	return out, err
}

// ListTraceIssuesBySession returns every detector finding for a session.
func (d *DB) ListTraceIssuesBySession(sessionID string) ([]*TraceIssue, error) {
	var result []*TraceIssue
	err := d.gorm.Where("session_id = ?", sessionID).Order("wasted_ns DESC").Find(&result).Error
	return result, err
}

func (d *DB) Close() error {
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

type SessionSize struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	ApproxBytes int64  `json:"approx_bytes"`
	SpanCount   int    `json:"span_count"`
}

// GetStorageBreakdown returns per-table sizes via duckdb_tables() and a
// per-session estimate based on the serialised span attribute lengths.
func (d *DB) GetStorageBreakdown() (*StorageBreakdown, error) {
	out := &StorageBreakdown{}

	// Per-table: estimated_size from DuckDB's internal catalogue.
	type tblSize struct {
		TableName     string
		EstimatedSize int64
	}
	var sizes []tblSize
	if err := d.gorm.Raw(`
		SELECT table_name, estimated_size
		FROM duckdb_tables()
		WHERE schema_name = 'main'
		ORDER BY estimated_size DESC
	`).Scan(&sizes).Error; err == nil {
		tableNames := []string{"spans", "logs", "metrics", "span_events", "span_links", "sessions", "trace_issues", "lint_warnings"}
		approxByTable := make(map[string]int64, len(tableNames))
		for _, s := range sizes {
			approxByTable[s.TableName] = s.EstimatedSize
		}
		for _, name := range tableNames {
			var cnt int64
			_ = d.gorm.Table(name).Count(&cnt).Error
			out.Tables = append(out.Tables, TableStat{
				Name:        name,
				RowCount:    cnt,
				ApproxBytes: approxByTable[name],
			})
		}
	}

	// Per-session: proxy size via span attribute payload length.
	var sessSizes []SessionSize
	_ = d.gorm.Raw(`
		SELECT s.session_id AS id, COALESCE(se.label,'') AS label, COUNT(*) AS span_count,
		       SUM(LENGTH(s.attributes::VARCHAR) + LENGTH(s.resource::VARCHAR)) AS approx_bytes
		FROM spans s
		LEFT JOIN sessions se ON se.id = s.session_id
		GROUP BY s.session_id, se.label
		ORDER BY approx_bytes DESC
		LIMIT 10
	`).Scan(&sessSizes).Error
	out.Sessions = sessSizes

	// File sizes: main DB file and WAL (if present).
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			out.MainBytes = fi.Size()
		}
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
	if err := d.gorm.Exec("CHECKPOINT").Error; err != nil {
		return res, fmt.Errorf("checkpoint: %w", err)
	}
	if err := d.gorm.Exec("VACUUM").Error; err != nil {
		return res, fmt.Errorf("vacuum: %w", err)
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
