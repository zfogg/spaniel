package storage

// SearchResult is one item returned by the global search.
type SearchResult struct {
	Kind      string `json:"kind"`              // "trace" | "log"
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id,omitempty"` // set for log results
	Title     string `json:"title"`             // span name or log body snippet
	Subtitle  string `json:"subtitle"`          // service name
	SessionID string `json:"session_id"`
}

// Search runs a cross-table search against spans and logs.
// query is matched with ILIKE on span name, service name, trace ID prefix,
// JSON attributes, and log body. sessionID limits results to a single session
// when non-empty.
func (d *DB) Search(query, sessionID string, limit int) ([]*SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pat := "%" + query + "%"
	tracePrefix := query + "%"
	half := limit / 2
	if half < 5 {
		half = 5
	}

	var results []*SearchResult

	// Trace / span search — group to one result per trace.
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
	`, sessionID, sessionID, pat, pat, tracePrefix, pat, half)
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

	// Log search.
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
	`, sessionID, sessionID, pat, half)
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
