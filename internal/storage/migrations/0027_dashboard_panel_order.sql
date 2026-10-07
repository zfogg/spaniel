-- Keep mutable positions out of ART indexes: DuckDB 1.1 implements indexed
-- updates as delete/insert, which conflicts with the panel primary key.
DROP INDEX IF EXISTS idx_dashboard_panels_order;
CREATE INDEX IF NOT EXISTS idx_dashboard_panels_dashboard ON dashboard_panels(dashboard_id);
