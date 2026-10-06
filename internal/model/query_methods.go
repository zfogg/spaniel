package model

import "gorm.io/gen"

// SpanEventMethods and SpanLinkMethods are the named, source-owned read
// queries whose shapes are stable enough to generate rather than assemble at
// runtime. The names become part of the storage API and remain low-cardinality
// even when callers supply different IDs.
type SpanEventMethods interface {
	// ListBySpan
	//
	// SELECT span_id, trace_id, session_id, time_ns, name, attributes::VARCHAR AS attributes FROM @@table WHERE span_id = @spanID ORDER BY time_ns ASC
	ListBySpan(spanID string) ([]gen.T, error)
}

type SpanLinkMethods interface {
	// ListBySpan
	//
	// SELECT span_id, trace_id, session_id, linked_trace_id, linked_span_id, trace_state, attributes::VARCHAR AS attributes FROM @@table WHERE span_id = @spanID
	ListBySpan(spanID string) ([]gen.T, error)

	// ListIncomingByTrace
	//
	// SELECT span_id, trace_id, session_id, linked_trace_id, linked_span_id, trace_state, attributes::VARCHAR AS attributes FROM @@table WHERE linked_trace_id = @traceID
	ListIncomingByTrace(traceID string) ([]gen.T, error)

	// ListByTrace
	//
	// SELECT span_id, trace_id, session_id, linked_trace_id, linked_span_id, trace_state, attributes::VARCHAR AS attributes FROM @@table WHERE trace_id = @traceID
	ListByTrace(traceID string) ([]gen.T, error)
}

// SpanSearchMethods contains the cross-table search projections. Keeping SQL
// here lets Gen emit stable, source-authored method names instead of leaving
// ad-hoc Raw calls in the storage service.
type SpanSearchMethods interface {
	// SearchTraces
	//
	// SELECT 'trace' AS kind, trace_id, '' AS span_id, MIN(name) AS title, MIN(service_name) AS subtitle, MIN(session_id) AS session_id FROM spans WHERE (@sessionID = '' OR session_id = @sessionID) AND (name ILIKE @pattern OR service_name ILIKE @pattern OR trace_id ILIKE @tracePrefix OR attributes::VARCHAR ILIKE @pattern) GROUP BY trace_id ORDER BY MAX(start_ns) DESC LIMIT @limit
	SearchTraces(sessionID, pattern, tracePrefix string, limit int) ([]SearchResult, error)

	// SearchChildSpans
	//
	// SELECT 'span' AS kind, trace_id, span_id, name AS title, service_name AS subtitle, session_id FROM spans WHERE (@sessionID = '' OR session_id = @sessionID) AND parent_span_id IS NOT NULL AND parent_span_id != '' AND (name ILIKE @pattern OR attributes::VARCHAR ILIKE @pattern) ORDER BY start_ns DESC LIMIT @limit
	SearchChildSpans(sessionID, pattern string, limit int) ([]SearchResult, error)

	// SearchServices
	//
	// SELECT 'service' AS kind, '' AS trace_id, '' AS span_id, service_name AS title, CONCAT(CAST(COUNT(*) AS VARCHAR), ' spans') AS subtitle, COALESCE(MIN(session_id), '') AS session_id FROM spans WHERE service_name ILIKE @pattern AND (@sessionID = '' OR session_id = @sessionID) GROUP BY service_name ORDER BY COUNT(*) DESC LIMIT @limit
	SearchServices(sessionID, pattern string, limit int) ([]SearchResult, error)
}

type SpanOverlayMethods interface {
	// ListTraceOverlays
	//
	// SELECT trace_id, name AS op, service_name AS service, status_code, start_ns, end_ns, duration_ns FROM @@table WHERE (parent_span_id = '' OR parent_span_id IS NULL) AND (@service = '' OR service_name = @service) AND (@sessionID = '' OR session_id = @sessionID) AND (@fromNs = 0 OR start_ns >= @fromNs) AND (@toNs = 0 OR start_ns <= @toNs) ORDER BY start_ns ASC LIMIT @limit
	ListTraceOverlays(service, sessionID string, fromNs, toNs int64, limit int) ([]TraceOverlay, error)
}

type SpanListMethods interface {
	// ListTagged
	//
	// WITH page_spans AS (SELECT trace_id, span_id, parent_span_id, service_name, name, kind, start_ns, end_ns, duration_ns, status_code, status_message, attributes, resource, session_id, session_label, received_at FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) AND (@service = '' OR service_name = @service) AND (@name = '' OR name = @name) AND (@hasKind = FALSE OR kind = @kind) ORDER BY CASE WHEN @sort = 'name' THEN name END ASC, CASE WHEN @sort = 'dur' THEN duration_ns END DESC, CASE WHEN @sort NOT IN ('name', 'dur') THEN start_ns END DESC LIMIT @limit OFFSET @offset) SELECT s.trace_id, s.span_id, s.parent_span_id, s.service_name, s.name, s.kind, s.start_ns, s.end_ns, s.duration_ns, s.status_code, s.status_message, s.attributes::VARCHAR AS attributes, s.resource::VARCHAR AS resource, s.session_id, s.session_label, s.received_at, CASE WHEN ni.trace_id IS NOT NULL THEN 'n+1' WHEN s.status_code = 2 THEN 'error' WHEN s.duration_ns > 250000000 THEN 'slow' WHEN lw.span_id IS NOT NULL THEN 'lint' ELSE '' END AS tag FROM page_spans s LEFT JOIN (SELECT DISTINCT trace_id FROM trace_issues WHERE kind = 'n_plus_one') ni ON s.trace_id = ni.trace_id LEFT JOIN (SELECT DISTINCT span_id FROM lint_warnings) lw ON s.span_id = lw.span_id ORDER BY CASE WHEN @sort = 'name' THEN s.name END ASC, CASE WHEN @sort = 'dur' THEN s.duration_ns END DESC, CASE WHEN @sort NOT IN ('name', 'dur') THEN s.start_ns END DESC
	ListTagged(sessionID, service, name string, hasKind bool, kind int, sort string, limit, offset int) ([]SpanRow, error)

	// ListGroups
	//
	// SELECT service_name, name, kind, COUNT(*) AS count, MAX(start_ns) AS latest_start_ns, SUM(CASE WHEN status_code = 2 THEN 1 ELSE 0 END) AS error_count, CAST(quantile_cont(duration_ns, 0.5) AS BIGINT) AS p50_duration_ns, CAST(quantile_cont(duration_ns, 0.95) AS BIGINT) AS p95_duration_ns, MAX(duration_ns) AS max_duration_ns, COUNT(DISTINCT attributes) AS attribute_variants FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) GROUP BY service_name, name, kind ORDER BY latest_start_ns DESC, service_name, name LIMIT @limit OFFSET @offset
	ListGroups(sessionID string, limit, offset int) ([]SpanGroup, error)

	// ListTraces
	//
	// WITH root_spans AS (SELECT trace_id, service_name, name, attributes::VARCHAR AS attributes, status_code, start_ns, end_ns, duration_ns, session_id, session_label FROM @@table WHERE (parent_span_id = '' OR parent_span_id IS NULL) AND (@sessionID = '' OR session_id = @sessionID) AND (@service = '' OR service_name = @service) ORDER BY start_ns DESC LIMIT @limit OFFSET @offset) SELECT rs.*, COALESCE(ti.has_n1, FALSE) AS has_n1, COALESCE(ti.issue_kinds_raw, '') AS issue_kinds_raw, COALESCE(sc.span_count, 1) AS span_count FROM root_spans rs LEFT JOIN (SELECT trace_id, BOOL_OR(kind = 'n_plus_one') AS has_n1, string_agg(DISTINCT kind, ',') AS issue_kinds_raw FROM trace_issues WHERE trace_id IN (SELECT trace_id FROM root_spans) GROUP BY trace_id) ti ON rs.trace_id = ti.trace_id LEFT JOIN (SELECT trace_id, COUNT(*) AS span_count FROM spans WHERE trace_id IN (SELECT trace_id FROM root_spans) GROUP BY trace_id) sc ON rs.trace_id = sc.trace_id ORDER BY rs.start_ns DESC
	ListTraces(sessionID, service string, limit, offset int) ([]TraceListRow, error)

	// CountGroups
	//
	// SELECT COUNT(*) AS count FROM (SELECT 1 FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) GROUP BY service_name, name, kind) groups
	CountGroups(sessionID string) ([]CountValue, error)

	// ListSourceStats
	//
	// SELECT service_name, COUNT(*) AS span_count, COUNT(*) FILTER (WHERE status_code = 2) AS error_count, SUM(LENGTH(attributes) + LENGTH(resource)) AS bytes_total, MIN(received_at) AS first_seen, MAX(received_at) AS last_seen FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) GROUP BY service_name ORDER BY span_count DESC
	ListSourceStats(sessionID string) ([]SourceStatsRow, error)
}

type SessionSearchMethods interface {
	// SearchByLabel
	//
	// SELECT 'session' AS kind, '' AS trace_id, '' AS span_id, label AS title, CASE WHEN is_baseline THEN 'baseline' ELSE 'session' END AS subtitle, id AS session_id FROM @@table WHERE label ILIKE @pattern ORDER BY created_at DESC LIMIT @limit
	SearchByLabel(pattern string, limit int) ([]SearchResult, error)
}

type SessionMethods interface {
	// GetByID
	//
	// SELECT id, label, created_at, is_baseline, is_imported, span_count, services::VARCHAR AS services, COALESCE(note, '') AS note, COALESCE(last_activity_ns, 0) AS last_activity_ns FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) ([]gen.T, error)

	// ListWithStats
	//
	// SELECT s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services::VARCHAR AS services, COALESCE(s.note, '') AS note, COALESCE(s.last_activity_ns, 0) AS last_activity_ns, COUNT(DISTINCT sp.trace_id) AS trace_count, CAST(COALESCE(QUANTILE_CONT(sp.duration_ns, 0.95), 0) AS BIGINT) AS p95_ns, COALESCE(SUM(LENGTH(sp.attributes::VARCHAR) + LENGTH(sp.resource::VARCHAR)), 0) AS size_bytes, COALESCE(ni.n1_count, 0) AS n1_count, COUNT(*) FILTER (WHERE sp.status_code = 2) AS error_count FROM @@table s LEFT JOIN spans sp ON sp.session_id = s.id LEFT JOIN (SELECT session_id, COUNT(*) AS n1_count FROM trace_issues WHERE kind = 'n_plus_one' GROUP BY session_id) ni ON ni.session_id = s.id GROUP BY s.id, s.label, s.created_at, s.is_baseline, s.is_imported, s.span_count, s.services, s.note, s.last_activity_ns, ni.n1_count ORDER BY s.created_at DESC
	ListWithStats() ([]SessionSummary, error)
}

type LogSearchMethods interface {
	// SearchByBody
	//
	// SELECT 'log' AS kind, trace_id, span_id, LEFT(body, 120) AS title, service_name AS subtitle, session_id FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) AND body ILIKE @pattern ORDER BY timestamp_ns DESC LIMIT @limit
	SearchByBody(sessionID, pattern string, limit int) ([]SearchResult, error)
}

type MetricMethods interface {
	// ListCatalog
	//
	// SELECT name, service_name, type, unit, description, any_value(aggregation_temporality) AS aggregation_temporality, any_value(is_monotonic) AS is_monotonic, COUNT(*) AS sample_count FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) GROUP BY name, service_name, type, unit, description ORDER BY service_name, name
	ListCatalog(sessionID string) ([]MetricCatalogEntry, error)
}

type SpanMetricMethods interface {
	// ServiceP95
	//
	// SELECT CAST(COALESCE(QUANTILE_CONT(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns FROM @@table WHERE service_name = @serviceName
	ServiceP95(serviceName string) ([]P95Value, error)
}

type SpanServiceMapMethods interface {
	// ListServiceMapNodes
	//
	// SELECT service_name AS id, COUNT(*) AS span_count, COUNT(*) FILTER (WHERE status_code = 2) AS error_count, CAST(COALESCE(QUANTILE_CONT(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) GROUP BY service_name ORDER BY service_name
	ListServiceMapNodes(sessionID string) ([]ServiceMapNode, error)

	// ListServiceMapEdges
	//
	// SELECT p.service_name AS "from", c.service_name AS "to", COUNT(*) AS call_count, CAST(AVG(c.duration_ns) AS BIGINT) AS avg_duration_ns, COUNT(*) FILTER (WHERE c.status_code = 2) AS error_count FROM @@table c INNER JOIN spans p ON c.parent_span_id = p.span_id WHERE c.service_name != p.service_name AND (@sessionID = '' OR c.session_id = @sessionID) GROUP BY p.service_name, c.service_name
	ListServiceMapEdges(sessionID string) ([]ServiceMapEdge, error)

	// ListTopOperations
	//
	// SELECT name, COUNT(*) AS count, CAST(COALESCE(QUANTILE_CONT(duration_ns, 0.95), 0) AS BIGINT) AS p95_ns FROM @@table WHERE service_name = @service AND (@sessionID = '' OR session_id = @sessionID) GROUP BY name ORDER BY count DESC LIMIT @limit
	ListTopOperations(service, sessionID string, limit int) ([]ServiceMapOpStat, error)
}

type SpanStorageMethods interface {
	// StorageTableSizes
	//
	// SELECT 'spans' AS name, COUNT(*) AS row_count, CAST(COALESCE(SUM(LENGTH(attributes::VARCHAR) + LENGTH(resource::VARCHAR) + LENGTH(name) + 300), 0) AS BIGINT) AS payload_bytes FROM @@table UNION ALL SELECT 'logs', COUNT(*), CAST(COALESCE(SUM(LENGTH(body) + LENGTH(attributes::VARCHAR) + 100), 0) AS BIGINT) FROM logs UNION ALL SELECT 'metrics', COUNT(*), CAST(COALESCE(SUM(LENGTH(attributes::VARCHAR) + LENGTH(name) + 80), 0) AS BIGINT) FROM metrics UNION ALL SELECT 'span_events', COUNT(*), CAST(COALESCE(SUM(LENGTH(attributes::VARCHAR) + LENGTH(name) + 80), 0) AS BIGINT) FROM span_events UNION ALL SELECT 'span_links', COUNT(*), CAST(COALESCE(SUM(LENGTH(attributes::VARCHAR) + 120), 0) AS BIGINT) FROM span_links UNION ALL SELECT 'sessions', COUNT(*), CAST(COUNT(*) * 200 AS BIGINT) FROM sessions UNION ALL SELECT 'trace_issues', COUNT(*), CAST(COALESCE(SUM(LENGTH(fingerprint) + 150), 0) AS BIGINT) FROM trace_issues UNION ALL SELECT 'lint_warnings', COUNT(*), CAST(COALESCE(SUM(LENGTH(message) + 100), 0) AS BIGINT) FROM lint_warnings
	StorageTableSizes() ([]TableSizeRow, error)

	// TopSessionSizes
	//
	// SELECT s.session_id AS id, COALESCE(se.label, '') AS label, COUNT(*) AS span_count, SUM(LENGTH(s.attributes::VARCHAR) + LENGTH(s.resource::VARCHAR)) AS approx_bytes FROM @@table s LEFT JOIN sessions se ON se.id = s.session_id GROUP BY s.session_id, se.label ORDER BY approx_bytes DESC LIMIT 10
	TopSessionSizes() ([]SessionSize, error)
}

type MetaDiagnosticMethods interface {
	// DatabaseSize
	//
	// SELECT block_size, used_blocks FROM pragma_database_size() LIMIT 1
	DatabaseSize() ([]DatabaseSize, error)
}

type TraceIssueSearchMethods interface {
	// SearchNPlusOne
	//
	// SELECT ti.trace_id, COALESCE(s.name, '(trace)') AS title, ti.count, ti.session_id FROM @@table ti LEFT JOIN spans s ON s.span_id = ti.example_span_id WHERE ti.kind = 'n_plus_one' AND (@sessionID = '' OR ti.session_id = @sessionID) ORDER BY ti.wasted_ns DESC LIMIT @limit
	SearchNPlusOne(sessionID string, limit int) ([]NPlusOneSearchResult, error)
}

type LintWarningSearchMethods interface {
	// SearchByRule
	//
	// SELECT lw.trace_id, COALESCE(MIN(s.name), '(trace)') AS title, MIN(lw.rule_id) AS rule_id, MIN(lw.session_id) AS session_id FROM @@table lw LEFT JOIN spans s ON s.span_id = lw.span_id WHERE lw.rule_id ILIKE @pattern AND (@sessionID = '' OR lw.session_id = @sessionID) AND lw.trace_id != '' GROUP BY lw.trace_id LIMIT @limit
	SearchByRule(sessionID, pattern string, limit int) ([]LintSearchResult, error)
}

type LintWarningMethods interface {
	// ListWithTraceIssues
	//
	// SELECT span_id, trace_id, session_id, rule_id, message, severity, created_at FROM @@table WHERE (@sessionID = '' OR session_id = @sessionID) UNION ALL SELECT example_span_id AS span_id, trace_id, session_id, kind AS rule_id, CASE kind WHEN 'n_plus_one' THEN 'N+1 query: ' || CAST(count AS VARCHAR) || ' repeated executions wasting ' || CAST(ROUND(wasted_ns / 1e6, 1) AS VARCHAR) || 'ms — ' || fingerprint ELSE kind || ': ' || CAST(count AS VARCHAR) || ' occurrences — ' || fingerprint END AS message, 'warning' AS severity, created_at FROM trace_issues WHERE (@sessionID = '' OR session_id = @sessionID) ORDER BY created_at DESC LIMIT 500
	ListWithTraceIssues(sessionID string) ([]gen.T, error)
}
