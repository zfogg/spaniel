-- The ingestion hot path now writes spans/logs/metrics via the DuckDB Appender
-- API. The appender binds a Go string to a JSON column as an escaped JSON string
-- literal (double-encoding it), whereas these columns are only ever read back as
-- ::VARCHAR and matched with ILIKE — the JSON type bought nothing. Convert them
-- to VARCHAR so the appender stores the serialized JSON verbatim. Casting the
-- existing JSON values to VARCHAR yields their serialized form (e.g. {"k":"v"}),
-- so stored data is preserved, not re-escaped.
ALTER TABLE spans ALTER attributes TYPE VARCHAR;
ALTER TABLE spans ALTER resource TYPE VARCHAR;
ALTER TABLE logs ALTER attributes TYPE VARCHAR;
ALTER TABLE metrics ALTER attributes TYPE VARCHAR;
