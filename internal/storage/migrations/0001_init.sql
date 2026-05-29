CREATE TABLE IF NOT EXISTS spans (
    trace_id        TEXT,
    span_id         TEXT,
    parent_span_id  TEXT,
    service_name    TEXT,
    name            TEXT,
    kind            INTEGER,
    start_ns        BIGINT,
    end_ns          BIGINT,
    duration_ns     BIGINT GENERATED ALWAYS AS (end_ns - start_ns),
    status_code     INTEGER,
    status_message  TEXT,
    attributes      VARCHAR,
    resource        VARCHAR,
    session_id      TEXT,
    session_label   TEXT,
    received_at     BIGINT,
    sampled         BOOLEAN DEFAULT TRUE
);
CREATE TABLE IF NOT EXISTS logs (
    timestamp_ns  BIGINT,
    trace_id      TEXT,
    span_id       TEXT,
    severity      INTEGER,
    body          TEXT,
    attributes    VARCHAR,
    service_name  TEXT,
    session_id    TEXT,
    received_at   BIGINT
);
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    label         TEXT,
    created_at    BIGINT,
    is_baseline   BOOLEAN DEFAULT FALSE,
    is_imported   BOOLEAN DEFAULT FALSE,
    span_count    INTEGER DEFAULT 0,
    services      JSON
);
CREATE TABLE IF NOT EXISTS lint_warnings (
    span_id     TEXT,
    trace_id    TEXT,
    session_id  TEXT,
    rule_id     TEXT,
    message     TEXT,
    severity    TEXT,
    created_at  BIGINT
);
CREATE TABLE IF NOT EXISTS trace_issues (
    id              VARCHAR PRIMARY KEY,
    trace_id        VARCHAR NOT NULL,
    session_id      VARCHAR NOT NULL,
    kind            VARCHAR NOT NULL,
    fingerprint     VARCHAR NOT NULL,
    count           INTEGER NOT NULL,
    wasted_ns       BIGINT NOT NULL,
    parent_span_id  VARCHAR NOT NULL DEFAULT '',
    example_span_id VARCHAR NOT NULL DEFAULT '',
    created_at      BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS span_events (
    span_id      TEXT,
    trace_id     TEXT,
    session_id   TEXT,
    time_ns      BIGINT,
    name         TEXT,
    attributes   JSON
);
CREATE INDEX IF NOT EXISTS idx_span_events_span_id ON span_events(span_id);
CREATE TABLE IF NOT EXISTS span_links (
    span_id          TEXT,
    trace_id         TEXT,
    session_id       TEXT,
    linked_trace_id  TEXT,
    linked_span_id   TEXT,
    trace_state      TEXT,
    attributes       JSON
);
CREATE INDEX IF NOT EXISTS idx_span_links_span_id ON span_links(span_id);
CREATE INDEX IF NOT EXISTS idx_span_links_linked_trace_id ON span_links(linked_trace_id);
CREATE TABLE IF NOT EXISTS metrics (
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
CREATE TABLE IF NOT EXISTS meta (
    meta_key   TEXT PRIMARY KEY,
    meta_value TEXT
);
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS is_imported BOOLEAN DEFAULT FALSE;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS note TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_activity_ns BIGINT DEFAULT 0;
-- sampled is added by migration 0003. Ensure it here too so pre-gormigrate
-- databases (which reach the schema via InitSchema, not the numbered
-- migrations) have the full current spans schema the Appender writes to.
ALTER TABLE spans ADD COLUMN IF NOT EXISTS sampled BOOLEAN DEFAULT TRUE;
-- The hot-path columns are VARCHAR (see 0005) so the Appender stores serialized
-- JSON verbatim. These ALTERs are idempotent (VARCHAR→VARCHAR is a no-op) and
-- ensure pre-gormigrate databases — which reach this file via InitSchema and so
-- never run migration 0005 — are also converted.
ALTER TABLE spans ALTER attributes TYPE VARCHAR;
ALTER TABLE spans ALTER resource TYPE VARCHAR;
ALTER TABLE logs ALTER attributes TYPE VARCHAR;
ALTER TABLE metrics ALTER attributes TYPE VARCHAR;
