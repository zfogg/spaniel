DELETE FROM dashboard_panels WHERE query_sql IS NULL OR trim(query_sql) = '';
DELETE FROM alert_rules WHERE query_sql IS NULL OR trim(query_sql) = '';
DROP INDEX IF EXISTS idx_dashboard_panels_order;
ALTER TABLE dashboard_panels DROP COLUMN IF EXISTS query_text;
ALTER TABLE dashboard_panels DROP COLUMN IF EXISTS query_json;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS query_json;
CREATE INDEX IF NOT EXISTS idx_dashboard_panels_order ON dashboard_panels(dashboard_id, position);
