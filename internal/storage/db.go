package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

type DB struct {
	db                 *sql.DB
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
	SpanCount  int    `json:"span_count"`
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
}

type Stats struct {
	SpanCount  int `json:"span_count"`
	TraceCount int `json:"trace_count"`
	LogCount   int `json:"log_count"`
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	d := &DB{db: db}
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
	`)
	return err
}

func (d *DB) CreateSession(label string, isBaseline bool) (*Session, error) {
	now := time.Now().UnixNano()
	id := fmt.Sprintf("session_%d", time.Now().UnixMilli())
	if label == "" {
		label = id
	}
	services, _ := json.Marshal([]string{})
	_, err := d.db.Exec(
		`INSERT INTO sessions (id, label, created_at, is_baseline, span_count, services) VALUES (?, ?, ?, ?, 0, ?)`,
		id, label, now, isBaseline, string(services),
	)
	if err != nil {
		return nil, err
	}
	return &Session{ID: id, Label: label, CreatedAt: now, IsBaseline: isBaseline, Services: string(services)}, nil
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
		SELECT trace_id, service_name, name, status_code, start_ns, end_ns, duration_ns, session_id, session_label
		FROM spans
		WHERE (parent_span_id = '' OR parent_span_id IS NULL)`
	args := []any{}

	if f.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, f.SessionID)
	}
	if f.Service != "" {
		query += ` AND service_name = ?`
		args = append(args, f.Service)
	}
	query += ` ORDER BY start_ns DESC LIMIT ? OFFSET ?`
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
			&t.StartNs, &t.EndNs, &t.DurationNs, &t.SessionID, &t.SessionLabel); err != nil {
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
		       attributes, resource, session_id, session_label, received_at
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
		       attributes, resource, session_id, session_label, received_at
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
		SELECT timestamp_ns, trace_id, span_id, severity, body, attributes, service_name, session_id, received_at
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
	rows, err := d.db.Query(`SELECT id, label, created_at, is_baseline, span_count, services FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(&s.ID, &s.Label, &s.CreatedAt, &s.IsBaseline, &s.SpanCount, &s.Services); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (d *DB) GetSession(id string) (*Session, error) {
	row := d.db.QueryRow(`SELECT id, label, created_at, is_baseline, span_count, services FROM sessions WHERE id = ?`, id)
	s := &Session{}
	err := row.Scan(&s.ID, &s.Label, &s.CreatedAt, &s.IsBaseline, &s.SpanCount, &s.Services)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
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
	return s, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}
