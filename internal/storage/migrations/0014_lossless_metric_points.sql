-- The original metrics table derived histogram percentiles and discarded OTLP
-- semantics. This is intentionally a clean break: metric history is retained
-- in sessions/traces/logs, but lossy metric rows are not migrated.
DROP TABLE IF EXISTS metrics;
CREATE TABLE metrics (
    name TEXT NOT NULL,
    description TEXT,
    unit TEXT,
    type TEXT NOT NULL,
    aggregation_temporality TEXT,
    is_monotonic BOOLEAN,
    start_timestamp_ns BIGINT,
    timestamp_ns BIGINT NOT NULL,
    flags UINTEGER,
    value DOUBLE,
    histogram_count UBIGINT,
    histogram_sum DOUBLE,
    histogram_min DOUBLE,
    histogram_max DOUBLE,
    explicit_bounds VARCHAR,
    bucket_counts VARCHAR,
    exp_scale INTEGER,
    exp_zero_count UBIGINT,
    exp_zero_threshold DOUBLE,
    exp_positive_offset INTEGER,
    exp_positive_counts VARCHAR,
    exp_negative_offset INTEGER,
    exp_negative_counts VARCHAR,
    summary_count UBIGINT,
    summary_sum DOUBLE,
    summary_quantiles VARCHAR,
    attributes VARCHAR,
    resource VARCHAR,
    series_attributes VARCHAR,
    series_key TEXT,
    scope_name TEXT,
    scope_version TEXT,
    scope_schema_url TEXT,
    scope_attributes VARCHAR,
    exemplars TEXT,
    service_name TEXT,
    session_id TEXT
);
CREATE INDEX idx_metric_points_lookup ON metrics(session_id, service_name, name, timestamp_ns);
CREATE INDEX idx_metric_points_series ON metrics(session_id, name, series_key, timestamp_ns);
CREATE INDEX idx_metric_points_global_lookup ON metrics(service_name, name, timestamp_ns);
CREATE INDEX idx_metric_points_name_time ON metrics(name, timestamp_ns);
