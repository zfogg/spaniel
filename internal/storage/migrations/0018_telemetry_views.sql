-- Stable read-only names for dashboard, MCP, and operator SQL.  The backing
-- table names remain implementation details while source-authored storage
-- queries use the generated model API.
CREATE OR REPLACE VIEW telemetry_spans AS SELECT * FROM spans;
CREATE OR REPLACE VIEW telemetry_logs AS SELECT * FROM logs;
CREATE OR REPLACE VIEW telemetry_metrics AS SELECT * FROM metrics;
CREATE OR REPLACE VIEW telemetry_traces AS
SELECT
    session_id,
    trace_id,
    MIN(start_ns) AS start_ns,
    MAX(end_ns) - MIN(start_ns) AS duration_ns,
    ARG_MIN(service_name, start_ns) AS service_name,
    ARG_MIN(name, start_ns) AS name,
    COUNT(*) AS span_count
FROM spans
WHERE trace_id IS NOT NULL AND trace_id <> ''
GROUP BY session_id, trace_id;
