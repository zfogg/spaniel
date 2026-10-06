-- 0015 adds scope_attributes to databases that already had the original
-- lossless metric schema. DuckDB appends ALTER-added columns, while the
-- Appender binds by ordinal position. Rebuild the table so scope_attributes
-- sits between scope_schema_url and exemplars, preserving every old column.
CREATE TABLE metrics_reordered (
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
INSERT INTO metrics_reordered (
    name, description, unit, type, aggregation_temporality, is_monotonic,
    start_timestamp_ns, timestamp_ns, flags, value,
    histogram_count, histogram_sum, histogram_min, histogram_max, explicit_bounds, bucket_counts,
    exp_scale, exp_zero_count, exp_zero_threshold, exp_positive_offset, exp_positive_counts,
    exp_negative_offset, exp_negative_counts, summary_count, summary_sum, summary_quantiles,
    attributes, resource, series_attributes, series_key,
    scope_name, scope_version, scope_schema_url, scope_attributes,
    exemplars, service_name, session_id
)
SELECT
    name, description, unit, type, aggregation_temporality, is_monotonic,
    start_timestamp_ns, timestamp_ns, flags, value,
    histogram_count, histogram_sum, histogram_min, histogram_max, explicit_bounds, bucket_counts,
    exp_scale, exp_zero_count, exp_zero_threshold, exp_positive_offset, exp_positive_counts,
    exp_negative_offset, exp_negative_counts, summary_count, summary_sum, summary_quantiles,
    attributes, resource, series_attributes, series_key,
    scope_name, scope_version, scope_schema_url, scope_attributes,
    exemplars, service_name, session_id
FROM metrics;
DROP TABLE metrics;
ALTER TABLE metrics_reordered RENAME TO metrics;
CREATE INDEX idx_metric_points_lookup ON metrics(session_id, service_name, name, timestamp_ns);
CREATE INDEX idx_metric_points_series ON metrics(session_id, name, series_key, timestamp_ns);
CREATE INDEX idx_metric_points_global_lookup ON metrics(service_name, name, timestamp_ns);
CREATE INDEX idx_metric_points_name_time ON metrics(name, timestamp_ns);
