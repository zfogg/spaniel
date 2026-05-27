package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

type DB struct {
	db                 *sql.DB
	path               string
	activeSessionID    string
	activeSessionLabel string
}

type Span struct {
	TraceID       string `json:"trace_id"`
	SpanID        string `json:"span_id"`
	ParentSpanID  string `json:"parent_span_id"`
	ServiceName   string `json:"service_name"`
	Name          string `json:"name"`
	Kind          int    `json:"kind"`
	StartNs       int64  `json:"start_ns"`
	EndNs         int64  `json:"end_ns"`
	DurationNs    int64  `json:"duration_ns"`
	StatusCode    int    `json:"status_code"`
	StatusMessage string `json:"status_message"`
	Attributes    string `json:"attributes"`
	Resource      string `json:"resource"`
	SessionID     string `json:"session_id"`
	SessionLabel  string `json:"session_label"`
	ReceivedAt    int64  `json:"received_at"`
}

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

type Session struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	IsBaseline bool   `json:"is_baseline"`
	IsImported bool   `json:"is_imported"`
	SpanCount  int    `json:"span_count"`
	TraceCount int    `json:"trace_count"`
	Services   string `json:"services"`
}

type LintWarning struct {
	SpanID    string `json:"span_id"`
	TraceID   string `json:"trace_id"`
	SessionID string `json:"session_id"`
	RuleID    string `json:"rule_id"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	CreatedAt int64  `json:"created_at"`
}

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
}

type TraceIssue struct {
	ID            string `json:"id"`
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

type Stats struct {
	SpanCount       int   `json:"span_count"`
	TraceCount      int   `json:"trace_count"`
	LogCount        int   `json:"log_count"`
	DBSize          int64 `json:"db_size"`
	SessionCount    int   `json:"session_count"`
	OldestSessionAt int64 `json:"oldest_session_at"`
}

type ServiceMapNode struct {
	ID         string `json:"id"`
	SpanCount  int    `json:"span_count"`
	ErrorCount int    `json:"error_count"`
}

type ServiceMapEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	CallCount     int    `json:"call_count"`
	AvgDurationNs int64  `json:"avg_duration_ns"`
}

type ServiceMapData struct {
	Nodes []*ServiceMapNode `json:"nodes"`
	Edges []*ServiceMapEdge `json:"edges"`
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	d := &DB{db: db, path: path}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS spans (
			trace_id        TEXT,
			span_id         TEXT,
			parent_span_id  TEXT,
			service_name    TEXT,
			name            TEXT,
			kind            INTEGER,
			start_ns        BIGINT,
			end_ns          BIGINT,
			duration_ns     BIGINT GENERATED ALWAYS AS (end_ns - start_ns),
			status_code     INTEGER,
			status_message  TEXT,
			attributes      JSON,
			resource        JSON,
			session_id      TEXT,
			session_label   TEXT,
			received_at     BIGINT
		);
		CREATE TABLE IF NOT EXISTS logs (
			timestamp_ns  BIGINT,
			trace_id      TEXT,
			span_id       TEXT,
			severity      INTEGER,
			body          TEXT,
			attributes    JSON,
			service_name  TEXT,
			session_id    TEXT,
			received_at   BIGINT
		);
		CREATE TABLE IF NOT EXISTS sessions (
			id            TEXT PRIMARY KEY,
			label         TEXT,
			created_at    BIGINT,
			is_baseline   BOOLEAN DEFAULT FALSE,
			is_imported   BOOLEAN DEFAULT FALSE,
			span_count    INTEGER DEFAULT 0,
			services      JSON
		);
		CREATE TABLE IF NOT EXISTS lint_warnings (
			span_id     TEXT,
			trace_id    TEXT,
			session_id  TEXT,
			rule_id     TEXT,
			message     TEXT,
			severity    TEXT,
			created_at  BIGINT
		);
		CREATE TABLE IF NOT EXISTS trace_issues (
			id             VARCHAR PRIMARY KEY,
			trace_id       VARCHAR NOT NULL,
			session_id     VARCHAR NOT NULL,
			kind           VARCHAR NOT NULL,
			fingerprint    VARCHAR NOT NULL,
			count          INTEGER NOT NULL,
			wasted_ns      BIGINT NOT NULL,
			parent_span_id VARCHAR NOT NULL DEFAULT '',
			example_span_id VARCHAR NOT NULL DEFAULT '',
			created_at     BIGINT NOT NULL
		);
	`)
	if err != nil {
		return err
	}
	// Add is_imported column for existing databases that predate this field.
	d.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS is_imported BOOLEAN DEFAULT FALSE`) //nolint:errcheck
	return nil
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
	id := fmt.Sprintf("session_%d", time.Now().UnixMilli())
	if label == "" {
		label = id
	}
	services, _ := json.Marshal([]string{})
	_, err := d.db.Exec(
		`INSERT INTO sessions (id, label, created_at, is_baseline, is_imported, span_count, services) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		id, label, now, isBaseline, isImported, string(services),
	)
	if err != nil {
		return nil, err
	}
	return &Session{ID: id, Label: label, CreatedAt: now, IsBaseline: isBaseline, IsImported: isImported, Services: string(services)}, nil
}

func (d *DB) SetActiveSession(id, label string) {
	d.activeSessionID = id
	d.activeSessionLabel = label
}

func (d *DB) ActiveSessionID() string    { return d.activeSessionID }
func (d *DB) ActiveSessionLabel() string { return d.activeSessionLabel }

func (d *DB) InsertSpan(s *Span) error {
	// duration_ns is a generated column — omit it from INSERT
	_, err := d.db.Exec(`
		INSERT INTO spans (
			trace_id, span_id, parent_span_id, service_name, name, kind,
			start_ns, end_ns, status_code, status_message,
			attributes, resource, session_id, session_label, received_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.TraceID, s.SpanID, s.ParentSpanID, s.ServiceName, s.Name, s.Kind,
		s.StartNs, s.EndNs, s.StatusCode, s.StatusMessage,
		s.Attributes, s.Resource, s.SessionID, s.SessionLabel, s.ReceivedAt,
	)
	return err
}

func (d *DB) InsertLog(l *Log) error {
	_, err := d.db.Exec(`
		INSERT INTO logs (timestamp_ns, trace_id, span_id, severity, body, attributes, service_name, session_id, received_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		l.TimestampNs, l.TraceID, l.SpanID, l.Severity, l.Body,
		l.Attributes, l.ServiceName, l.SessionID, l.ReceivedAt,
	)
	return err
}

func (d *DB) InsertLintWarning(w *LintWarning) error {
	_, err := d.db.Exec(`
		INSERT INTO lint_warnings (span_id, trace_id, session_id, rule_id, message, severity, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		w.SpanID, w.TraceID, w.SessionID, w.RuleID, w.Message, w.Severity, w.CreatedAt,
	)
	return err
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
		       COALESCE(ti.has_n1, FALSE) AS has_n1
		FROM spans s
		LEFT JOIN (
			SELECT trace_id, TRUE AS has_n1
			FROM trace_issues WHERE kind = 'n_plus_one'
			GROUP BY trace_id
		) ti ON s.trace_id = ti.trace_id
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

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*TraceRow
	for rows.Next() {
		t := &TraceRow{}
		if err := rows.Scan(&t.TraceID, &t.ServiceName, &t.Name, &t.StatusCode,
			&t.StartNs, &t.EndNs, &t.DurationNs, &t.SessionID, &t.SessionLabel, &t.HasN1); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (d *DB) GetTrace(traceID string) ([]*Span, error) {
	rows, err := d.db.Query(`
		SELECT trace_id, span_id, parent_span_id, service_name, name, kind,
		       start_ns, end_ns, duration_ns, status_code, status_message,
		       attributes::VARCHAR, resource::VARCHAR, session_id, session_label, received_at
		FROM spans WHERE trace_id = ? ORDER BY start_ns`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Span
	for rows.Next() {
		s := &Span{}
		if err := rows.Scan(
			&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.ServiceName, &s.Name, &s.Kind,
			&s.StartNs, &s.EndNs, &s.DurationNs, &s.StatusCode, &s.StatusMessage,
			&s.Attributes, &s.Resource, &s.SessionID, &s.SessionLabel, &s.ReceivedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (d *DB) GetSpan(spanID string) (*Span, error) {
	row := d.db.QueryRow(`
		SELECT trace_id, span_id, parent_span_id, service_name, name, kind,
		       start_ns, end_ns, duration_ns, status_code, status_message,
		       attributes::VARCHAR, resource::VARCHAR, session_id, session_label, received_at
		FROM spans WHERE span_id = ? LIMIT 1`, spanID)
	s := &Span{}
	err := row.Scan(
		&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.ServiceName, &s.Name, &s.Kind,
		&s.StartNs, &s.EndNs, &s.DurationNs, &s.StatusCode, &s.StatusMessage,
		&s.Attributes, &s.Resource, &s.SessionID, &s.SessionLabel, &s.ReceivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
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

	query := `
		SELECT timestamp_ns, trace_id, span_id, severity, body, attributes::VARCHAR, service_name, session_id, received_at
		FROM logs WHERE 1=1`
	args := []any{}

	if f.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, f.SessionID)
	}
	if f.TraceID != "" {
		query += ` AND trace_id = ?`
		args = append(args, f.TraceID)
	}
	if f.SpanID != "" {
		query += ` AND span_id = ?`
		args = append(args, f.SpanID)
	}
	query += ` ORDER BY timestamp_ns DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Log
	for rows.Next() {
		l := &Log{}
		if err := rows.Scan(&l.TimestampNs, &l.TraceID, &l.SpanID, &l.Severity, &l.Body,
			&l.Attributes, &l.ServiceName, &l.SessionID, &l.ReceivedAt); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

func (d *DB) ListServices() ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT service_name FROM spans ORDER BY service_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (d *DB) ListSessions() ([]*Session, error) {
	rows, err := d.db.Query(`
		SELECT s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services,
		       COUNT(DISTINCT sp.trace_id) AS trace_count
		FROM sessions s
		LEFT JOIN spans sp ON sp.session_id = s.id
		GROUP BY s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services
		ORDER BY s.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(&s.ID, &s.Label, &s.CreatedAt, &s.IsBaseline, &s.IsImported, &s.SpanCount, &s.Services, &s.TraceCount); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (d *DB) GetSession(id string) (*Session, error) {
	row := d.db.QueryRow(`SELECT id, label, created_at, is_baseline, is_imported, span_count, services FROM sessions WHERE id = ?`, id)
	s := &Session{}
	err := row.Scan(&s.ID, &s.Label, &s.CreatedAt, &s.IsBaseline, &s.IsImported, &s.SpanCount, &s.Services)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (d *DB) SetBaseline(id string, isBaseline bool) error {
	if isBaseline {
		// clear any previous baseline first
		if _, err := d.db.Exec(`UPDATE sessions SET is_baseline = FALSE WHERE is_baseline = TRUE`); err != nil {
			return err
		}
	}
	_, err := d.db.Exec(`UPDATE sessions SET is_baseline = ? WHERE id = ?`, isBaseline, id)
	return err
}

func (d *DB) DeleteSession(id string) error {
	for _, tbl := range []string{"lint_warnings", "trace_issues", "logs", "spans"} {
		if _, err := d.db.Exec(`DELETE FROM `+tbl+` WHERE session_id = ?`, id); err != nil {
			return err
		}
	}
	_, err := d.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (d *DB) ListLintWarnings(sessionID string) ([]*LintWarning, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if sessionID != "" {
		rows, err = d.db.Query(`
			SELECT span_id, trace_id, session_id, rule_id, message, severity, created_at
			FROM lint_warnings WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	} else {
		rows, err = d.db.Query(`
			SELECT span_id, trace_id, session_id, rule_id, message, severity, created_at
			FROM lint_warnings ORDER BY created_at DESC LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*LintWarning
	for rows.Next() {
		w := &LintWarning{}
		if err := rows.Scan(&w.SpanID, &w.TraceID, &w.SessionID, &w.RuleID, &w.Message, &w.Severity, &w.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (d *DB) GetStats(sessionID string) (*Stats, error) {
	s := &Stats{}
	var row *sql.Row
	if sessionID != "" {
		row = d.db.QueryRow(`SELECT COUNT(*) FROM spans WHERE session_id = ?`, sessionID)
	} else {
		row = d.db.QueryRow(`SELECT COUNT(*) FROM spans`)
	}
	if err := row.Scan(&s.SpanCount); err != nil {
		return nil, err
	}
	if sessionID != "" {
		row = d.db.QueryRow(`SELECT COUNT(DISTINCT trace_id) FROM spans WHERE session_id = ?`, sessionID)
	} else {
		row = d.db.QueryRow(`SELECT COUNT(DISTINCT trace_id) FROM spans`)
	}
	if err := row.Scan(&s.TraceCount); err != nil {
		return nil, err
	}
	if sessionID != "" {
		row = d.db.QueryRow(`SELECT COUNT(*) FROM logs WHERE session_id = ?`, sessionID)
	} else {
		row = d.db.QueryRow(`SELECT COUNT(*) FROM logs`)
	}
	if err := row.Scan(&s.LogCount); err != nil {
		return nil, err
	}
	if d.path != "" && d.path != ":memory:" {
		if fi, err := os.Stat(d.path); err == nil {
			s.DBSize = fi.Size()
		}
	}
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&s.SessionCount)
	var oldest sql.NullInt64
	_ = d.db.QueryRow(`SELECT MIN(created_at) FROM sessions`).Scan(&oldest)
	if oldest.Valid {
		s.OldestSessionAt = oldest.Int64
	}
	return s, nil
}

func (d *DB) GetServiceMap(sessionID string) (*ServiceMapData, error) {
	// Nodes
	nodeQuery := `
		SELECT service_name,
		       COUNT(*) AS span_count,
		       COUNT(*) FILTER (WHERE status_code = 2) AS error_count
		FROM spans`
	nodeArgs := []any{}
	if sessionID != "" {
		nodeQuery += ` WHERE session_id = ?`
		nodeArgs = append(nodeArgs, sessionID)
	}
	nodeQuery += ` GROUP BY service_name ORDER BY service_name`

	nrows, err := d.db.Query(nodeQuery, nodeArgs...)
	if err != nil {
		return nil, fmt.Errorf("service map nodes: %w", err)
	}
	defer nrows.Close()
	var nodes []*ServiceMapNode
	for nrows.Next() {
		n := &ServiceMapNode{}
		if err := nrows.Scan(&n.ID, &n.SpanCount, &n.ErrorCount); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if err := nrows.Err(); err != nil {
		return nil, err
	}

	// Edges: parent-child pairs that cross service boundaries
	edgeQuery := `
		SELECT p.service_name,
		       c.service_name,
		       COUNT(*) AS call_count,
		       CAST(AVG(c.duration_ns) AS BIGINT) AS avg_duration_ns
		FROM spans c
		INNER JOIN spans p ON c.parent_span_id = p.span_id
		WHERE c.service_name != p.service_name`
	edgeArgs := []any{}
	if sessionID != "" {
		edgeQuery += ` AND c.session_id = ?`
		edgeArgs = append(edgeArgs, sessionID)
	}
	edgeQuery += ` GROUP BY p.service_name, c.service_name`

	erows, err := d.db.Query(edgeQuery, edgeArgs...)
	if err != nil {
		return nil, fmt.Errorf("service map edges: %w", err)
	}
	defer erows.Close()
	var edges []*ServiceMapEdge
	for erows.Next() {
		e := &ServiceMapEdge{}
		if err := erows.Scan(&e.From, &e.To, &e.CallCount, &e.AvgDurationNs); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	if err := erows.Err(); err != nil {
		return nil, err
	}

	if nodes == nil {
		nodes = []*ServiceMapNode{}
	}
	if edges == nil {
		edges = []*ServiceMapEdge{}
	}
	return &ServiceMapData{Nodes: nodes, Edges: edges}, nil
}

func (d *DB) UpsertTraceIssue(issue *TraceIssue) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO trace_issues
			(id, trace_id, session_id, kind, fingerprint, count, wasted_ns, parent_span_id, example_span_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issue.ID, issue.TraceID, issue.SessionID, issue.Kind,
		issue.Fingerprint, issue.Count, issue.WastedNs,
		issue.ParentSpanID, issue.ExampleSpanID, issue.CreatedAt,
	)
	return err
}

func (d *DB) GetTraceIssues(traceID string) ([]*TraceIssue, error) {
	rows, err := d.db.Query(`
		SELECT id, trace_id, session_id, kind, fingerprint, count, wasted_ns,
		       parent_span_id, example_span_id, created_at
		FROM trace_issues WHERE trace_id = ? ORDER BY wasted_ns DESC`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*TraceIssue
	for rows.Next() {
		issue := &TraceIssue{}
		if err := rows.Scan(&issue.ID, &issue.TraceID, &issue.SessionID, &issue.Kind,
			&issue.Fingerprint, &issue.Count, &issue.WastedNs,
			&issue.ParentSpanID, &issue.ExampleSpanID, &issue.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, issue)
	}
	if result == nil {
		result = []*TraceIssue{}
	}
	return result, nil
}

// GetSpansBySession returns all spans for a session, ordered by start time.
func (d *DB) GetSpansBySession(sessionID string) ([]*Span, error) {
	rows, err := d.db.Query(`
		SELECT trace_id, span_id, parent_span_id, service_name, name, kind,
		       start_ns, end_ns, duration_ns, status_code, status_message,
		       attributes::VARCHAR, resource::VARCHAR, session_id, session_label, received_at
		FROM spans WHERE session_id = ? ORDER BY start_ns ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Span
	for rows.Next() {
		s := &Span{}
		if err := rows.Scan(
			&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.ServiceName, &s.Name, &s.Kind,
			&s.StartNs, &s.EndNs, &s.DurationNs, &s.StatusCode, &s.StatusMessage,
			&s.Attributes, &s.Resource, &s.SessionID, &s.SessionLabel, &s.ReceivedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (d *DB) Close() error {
	return d.db.Close()
}
