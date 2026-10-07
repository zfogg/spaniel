-- DuckDB cannot update a value referenced by this secondary index when the
-- table also has a composite primary key. Current alert state must be writable;
-- alert_events is the indexed historical query surface instead.
DROP INDEX IF EXISTS idx_alert_instances_state;
