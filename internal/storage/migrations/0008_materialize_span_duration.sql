-- DuckDB 1.4 can invalidate the entire database while checkpointing a table
-- with this generated expression ("Invalid node type for
-- TransformToDeprecated"). Materialize duration_ns instead. The ingestion
-- pipeline already computes it, and rebuilding also removes the incompatible
-- generated-column metadata from existing databases.
CREATE TABLE spans_materialized (
    trace_id        TEXT,
    span_id         TEXT,
    parent_span_id  TEXT,
    service_name    TEXT,
    name            TEXT,
    kind            INTEGER,
    start_ns        BIGINT,
    end_ns          BIGINT,
    duration_ns     BIGINT,
    status_code     INTEGER,
    status_message  TEXT,
    attributes      VARCHAR,
    resource        VARCHAR,
    session_id      TEXT,
    session_label   TEXT,
    received_at     BIGINT,
    sampled         BOOLEAN DEFAULT TRUE
);
INSERT INTO spans_materialized SELECT
    trace_id, span_id, parent_span_id, service_name, name, kind,
    start_ns, end_ns, end_ns - start_ns, status_code, status_message,
    attributes, resource, session_id, session_label, received_at, sampled
FROM spans;
DROP TABLE spans;
ALTER TABLE spans_materialized RENAME TO spans;

CREATE INDEX idx_spans_trace_id ON spans(trace_id);
CREATE INDEX idx_spans_parent_span_id ON spans(parent_span_id);
CREATE INDEX idx_spans_session_id ON spans(session_id);
CREATE INDEX idx_spans_start_ns ON spans(start_ns);
CREATE INDEX idx_spans_root_start ON spans(parent_span_id, start_ns);
CREATE INDEX idx_spans_session_start ON spans(session_id, start_ns);
CREATE INDEX idx_spans_trace_start ON spans(trace_id, start_ns);
CREATE INDEX idx_spans_trace_id_only ON spans(trace_id);
