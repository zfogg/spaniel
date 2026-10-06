-- SQL is the stable saved-query contract. The legacy text/JSON columns remain
-- temporarily so existing databases can be upgraded without a table rewrite.
ALTER TABLE dashboard_panels ADD COLUMN IF NOT EXISTS query_sql VARCHAR NOT NULL DEFAULT '';
ALTER TABLE dashboard_panels ADD COLUMN IF NOT EXISTS query_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS query_sql VARCHAR NOT NULL DEFAULT '';
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS query_version INTEGER NOT NULL DEFAULT 1;

-- Curated read-only SQL surface. These names are the dashboard/alert contract;
-- internal table layout can evolve behind them.
CREATE OR REPLACE VIEW telemetry_spans AS SELECT * FROM spans;
CREATE OR REPLACE VIEW telemetry_logs AS SELECT * FROM logs;
CREATE OR REPLACE VIEW telemetry_metrics AS SELECT * FROM metrics;
CREATE OR REPLACE VIEW telemetry_traces AS
SELECT * FROM spans WHERE parent_span_id = '' OR parent_span_id IS NULL;
