package storage

import (
	"fmt"
	"strings"
)

// SearchResult is one item returned by the global search.
type SearchResult struct {
	Kind      string `json:"kind"`              // "trace" | "span" | "session" | "service" | "log"
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id,omitempty"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	SessionID string `json:"session_id"`
}

// parseFilters splits a query into field:value filters and the remaining
// free-text terms. e.g. "lint:n+1 orders" -> {"lint":"n+1"}, "orders".
func parseFilters(query string) (map[string]string, string) {
	filters := map[string]string{}
	var rest []string
	for _, tok := range strings.Fields(query) {
		if k, v, ok := strings.Cut(tok, ":"); ok && k != "" && v != "" {
			filters[strings.ToLower(k)] = v
			continue
		}
		rest = append(rest, tok)
	}
	return filters, strings.Join(rest, " ")
}

// Search runs a cross-table search against spans, logs, sessions, and services.
// It also supports field:value filters; currently lint:<rule> (with the alias
// n+1 == n_plus_one) which returns traces flagged by the linter or detectors.
func (d *DB) Search(query, sessionID string, limit int) ([]*SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	filters, freeText := parseFilters(query)
	if rule, ok := filters["lint"]; ok {
		return d.searchLint(rule, sessionID, limit)
	}
	if freeText != "" {
		query = freeText
	}

	pat := "%" + query + "%"
	tracePrefix := query + "%"
	quarter := limit / 4
	if quarter < 3 {
		quarter = 3
	}

	var results []*SearchResult

	// ── Traces (spans grouped by trace_id) ───────────────────────────────────
	traceRows, err := d.db.Query(`
		SELECT
			'trace'           AS kind,
			trace_id,
			''                AS span_id,
			MIN(name)         AS title,
			MIN(service_name) AS subtitle,
			MIN(session_id)   AS session_id
		FROM spans
		WHERE (? = '' OR session_id = ?)
		  AND (
		      name         ILIKE ?
		   OR service_name ILIKE ?
		   OR trace_id     ILIKE ?
		   OR attributes::VARCHAR ILIKE ?
		  )
		GROUP BY trace_id
		ORDER BY MAX(start_ns) DESC
		LIMIT ?
	`, sessionID, sessionID, pat, pat, tracePrefix, pat, quarter)
	if err != nil {
		return nil, err
	}
	defer traceRows.Close()
	for traceRows.Next() {
		var r SearchResult
		var spanID string
		if err := traceRows.Scan(&r.Kind, &r.TraceID, &spanID, &r.Title, &r.Subtitle, &r.SessionID); err != nil {
			continue
		}
		results = append(results, &r)
	}

	// ── Spans (individual child spans) ────────────────────────────────────────
	spanRows, err := d.db.Query(`
		SELECT
			'span'       AS kind,
			trace_id,
			span_id,
			name         AS title,
			service_name AS subtitle,
			session_id
		FROM spans
		WHERE (? = '' OR session_id = ?)
		  AND parent_span_id IS NOT NULL AND parent_span_id != ''
		  AND (
		      name         ILIKE ?
		   OR attributes::VARCHAR ILIKE ?
		  )
		ORDER BY start_ns DESC
		LIMIT ?
	`, sessionID, sessionID, pat, pat, quarter)
	if err != nil {
		return nil, err
	}
	defer spanRows.Close()
	for spanRows.Next() {
		var r SearchResult
		if err := spanRows.Scan(&r.Kind, &r.TraceID, &r.SpanID, &r.Title, &r.Subtitle, &r.SessionID); err != nil {
			continue
		}
		results = append(results, &r)
	}

	// ── Sessions ──────────────────────────────────────────────────────────────
	sessRows, err := d.db.Query(`
		SELECT
			'session' AS kind,
			''        AS trace_id,
			''        AS span_id,
			label     AS title,
			CASE WHEN is_baseline THEN 'baseline' ELSE 'session' END AS subtitle,
			id        AS session_id
		FROM sessions
		WHERE label ILIKE ?
		ORDER BY created_at DESC
		LIMIT ?
	`, pat, quarter)
	if err != nil {
		return nil, err
	}
	defer sessRows.Close()
	for sessRows.Next() {
		var r SearchResult
		if err := sessRows.Scan(&r.Kind, &r.TraceID, &r.SpanID, &r.Title, &r.Subtitle, &r.SessionID); err != nil {
			continue
		}
		results = append(results, &r)
	}

	// ── Services ──────────────────────────────────────────────────────────────
	svcRows, err := d.db.Query(`
		SELECT
			'service'    AS kind,
			''           AS trace_id,
			''           AS span_id,
			service_name AS title,
			CONCAT(CAST(COUNT(*) AS VARCHAR), ' spans') AS subtitle,
			COALESCE(MIN(session_id), '') AS session_id
		FROM spans
		WHERE service_name ILIKE ?
		  AND (? = '' OR session_id = ?)
		GROUP BY service_name
		ORDER BY COUNT(*) DESC
		LIMIT ?
	`, pat, sessionID, sessionID, quarter)
	if err != nil {
		return nil, err
	}
	defer svcRows.Close()
	for svcRows.Next() {
		var r SearchResult
		if err := svcRows.Scan(&r.Kind, &r.TraceID, &r.SpanID, &r.Title, &r.Subtitle, &r.SessionID); err != nil {
			continue
		}
		results = append(results, &r)
	}

	// ── Logs ──────────────────────────────────────────────────────────────────
	logRows, err := d.db.Query(`
		SELECT
			'log'              AS kind,
			trace_id,
			span_id,
			LEFT(body, 120)    AS title,
			service_name       AS subtitle,
			session_id
		FROM logs
		WHERE (? = '' OR session_id = ?)
		  AND body ILIKE ?
		ORDER BY timestamp_ns DESC
		LIMIT ?
	`, sessionID, sessionID, pat, quarter)
	if err != nil {
		return nil, err
	}
	defer logRows.Close()
	for logRows.Next() {
		var r SearchResult
		if err := logRows.Scan(&r.Kind, &r.TraceID, &r.SpanID, &r.Title, &r.Subtitle, &r.SessionID); err != nil {
			continue
		}
		results = append(results, &r)
	}

	return results, nil
}

// searchLint returns traces flagged by the linter or detectors for the given
// rule. The rule "n+1" (and "n1") is aliased to the detector kind "n_plus_one";
// any other value is matched against lint_warnings.rule_id.
func (d *DB) searchLint(rule, sessionID string, limit int) ([]*SearchResult, error) {
	norm := strings.ToLower(strings.TrimSpace(rule))
	var results []*SearchResult

	if norm == "n+1" || norm == "n1" || norm == "n_plus_one" {
		rows, err := d.db.Query(`
			SELECT ti.trace_id,
			       COALESCE(s.name, '(trace)') AS title,
			       ti.count,
			       ti.session_id
			FROM trace_issues ti
			LEFT JOIN spans s ON s.span_id = ti.example_span_id
			WHERE ti.kind = 'n_plus_one'
			  AND (? = '' OR ti.session_id = ?)
			ORDER BY ti.wasted_ns DESC
			LIMIT ?`, sessionID, sessionID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r SearchResult
			var count int
			if err := rows.Scan(&r.TraceID, &r.Title, &count, &r.SessionID); err != nil {
				continue
			}
			r.Kind = "trace"
			r.Subtitle = fmt.Sprintf("N+1 · %d queries", count)
			results = append(results, &r)
		}
		return results, nil
	}

	// Other lint rules: match lint_warnings.rule_id, grouped by trace.
	rows, err := d.db.Query(`
		SELECT lw.trace_id,
		       COALESCE(MIN(s.name), '(trace)') AS title,
		       MIN(lw.rule_id) AS rule_id,
		       MIN(lw.session_id) AS session_id
		FROM lint_warnings lw
		LEFT JOIN spans s ON s.span_id = lw.span_id
		WHERE lw.rule_id ILIKE ?
		  AND (? = '' OR lw.session_id = ?)
		  AND lw.trace_id != ''
		GROUP BY lw.trace_id
		LIMIT ?`, "%"+rule+"%", sessionID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r SearchResult
		var ruleID string
		if err := rows.Scan(&r.TraceID, &r.Title, &ruleID, &r.SessionID); err != nil {
			continue
		}
		r.Kind = "trace"
		r.Subtitle = "lint · " + ruleID
		results = append(results, &r)
	}
	return results, nil
}
