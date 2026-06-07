-- Repair the metrics table's column set AND order to match the canonical schema
-- the columnar Appender writes to:
--   name, description, unit, type, timestamp_ns, value, attributes,
--   exemplars, service_name, session_id
--
-- Why this is needed: the DuckDB Appender (added in v0.2.0) binds values
-- positionally. Databases created before this fix are in one of two broken
-- shapes:
--   1. Missing `exemplars` entirely — born under an older InitSchema baseline
--      that stamped 0004_metrics_exemplars as applied without ever running it,
--      so 0004 can never re-run.
--   2. Has `exemplars` in the wrong position — ran 0004 incrementally, which
--      ALTER-ADDs the column last (slot 10) rather than slot 8.
-- Either way AppendMetric writes 10 values into a table whose columns don't line
-- up ("invalid column count" / wrong-column data). Rebuilding fixes both. This
-- runs under a fresh migration ID, so it applies even though 0004 is recorded.
--
-- Pre-existing exemplar values (only possible in shape 2) are not carried over
-- (they are optional outlier pointers and re-accumulate from new data).
CREATE TABLE metrics_canonical (
    name         TEXT,
    description  TEXT,
    unit         TEXT,
    type         TEXT,
    timestamp_ns BIGINT,
    value        DOUBLE,
    attributes   VARCHAR,
    exemplars    TEXT,
    service_name TEXT,
    session_id   TEXT
);
INSERT INTO metrics_canonical
    (name, description, unit, type, timestamp_ns, value, attributes, service_name, session_id)
SELECT
    name, description, unit, type, timestamp_ns, value, attributes, service_name, session_id
FROM metrics;
DROP TABLE metrics;
ALTER TABLE metrics_canonical RENAME TO metrics;
