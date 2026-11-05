-- Producer timestamps are not admission times; old rows intentionally remain unknown.
ALTER TABLE metric_series_catalog ADD COLUMN IF NOT EXISTS admitted_at_ns BIGINT;
CREATE OR REPLACE VIEW telemetry_metric_series AS SELECT * FROM metric_series_catalog;
CREATE TABLE IF NOT EXISTS metric_admission_observations (
    session_id TEXT NOT NULL,
    service_name TEXT NOT NULL,
    name TEXT NOT NULL,
    started_at_ns BIGINT NOT NULL,
    PRIMARY KEY (session_id, service_name, name)
);
