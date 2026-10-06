-- SQL is the stable saved-query contract. The legacy text/JSON columns remain
-- temporarily so existing databases can be upgraded without a table rewrite.
-- DuckDB cannot add a constrained column to an existing table. Add nullable
-- columns, then backfill existing rows; runtime validation owns new-row
-- defaults and non-empty SQL requirements.
ALTER TABLE dashboard_panels ADD COLUMN IF NOT EXISTS query_sql VARCHAR;
ALTER TABLE dashboard_panels ADD COLUMN IF NOT EXISTS query_version INTEGER;
UPDATE dashboard_panels SET query_sql = '' WHERE query_sql IS NULL;
UPDATE dashboard_panels SET query_version = 1 WHERE query_version IS NULL;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS query_sql VARCHAR;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS query_version INTEGER;
UPDATE alert_rules SET query_sql = '' WHERE query_sql IS NULL;
UPDATE alert_rules SET query_version = 1 WHERE query_version IS NULL;

-- Curated read-only SQL surface. These names are the dashboard/alert contract;
-- internal table layout can evolve behind them.
CREATE OR REPLACE VIEW telemetry_spans AS SELECT * FROM spans;
CREATE OR REPLACE VIEW telemetry_logs AS SELECT * FROM logs;
CREATE OR REPLACE VIEW telemetry_metrics AS SELECT * FROM metrics;
CREATE OR REPLACE VIEW telemetry_traces AS
SELECT * FROM spans WHERE parent_span_id = '' OR parent_span_id IS NULL;
