-- Stable public views for the remaining normalized telemetry records.
CREATE OR REPLACE VIEW telemetry_span_events AS SELECT * FROM span_events;
CREATE OR REPLACE VIEW telemetry_span_links AS SELECT * FROM span_links;
CREATE OR REPLACE VIEW telemetry_metric_series AS SELECT * FROM metric_series_catalog;
CREATE OR REPLACE VIEW telemetry_sessions AS SELECT * FROM sessions;
CREATE OR REPLACE VIEW telemetry_findings AS
SELECT session_id, 'lint_warning' AS source, severity, rule_id AS kind, message,
       trace_id, span_id, CAST(NULL AS BIGINT) AS count,
       CAST(NULL AS BIGINT) AS wasted_ns, created_at
FROM lint_warnings
UNION ALL
SELECT session_id, 'trace_issue' AS source, 'warning' AS severity, kind,
       kind AS message, trace_id, example_span_id AS span_id,
       CAST(count AS BIGINT) AS count, wasted_ns, created_at
FROM trace_issues;
