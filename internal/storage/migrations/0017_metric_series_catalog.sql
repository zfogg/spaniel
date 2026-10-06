-- Durable, bounded identities admitted by the ingestion cardinality policy.
-- It deliberately has no per-point values or arbitrary OTLP attributes.
CREATE TABLE metric_series_catalog (
    session_id TEXT NOT NULL,
    service_name TEXT NOT NULL,
    name TEXT NOT NULL,
    series_key TEXT NOT NULL,
    series_attributes VARCHAR NOT NULL,
    first_timestamp_ns BIGINT NOT NULL,
    last_timestamp_ns BIGINT NOT NULL,
    point_count BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (session_id, service_name, name, series_key)
);
CREATE INDEX idx_metric_series_catalog_stream ON metric_series_catalog(session_id, service_name, name);
CREATE INDEX idx_metric_series_catalog_name ON metric_series_catalog(name);
