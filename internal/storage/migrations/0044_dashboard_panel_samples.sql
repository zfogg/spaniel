CREATE TABLE IF NOT EXISTS dashboard_panel_samples (
    series_key VARCHAR NOT NULL,
    panel_id VARCHAR NOT NULL,
    session_id VARCHAR NOT NULL,
    bucket_ns BIGINT NOT NULL,
    value DOUBLE NOT NULL,
    PRIMARY KEY (series_key, bucket_ns)
);
CREATE INDEX IF NOT EXISTS idx_dashboard_panel_samples_time ON dashboard_panel_samples (bucket_ns);
