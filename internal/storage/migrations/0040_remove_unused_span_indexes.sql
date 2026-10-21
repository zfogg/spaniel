-- DuckDB's ART scan planner uses single-column indexes. These nonunique
-- composites cannot serve scans, and trace_id_only duplicates trace_id.
-- Retain the selective lookup indexes; remove allocation and write overhead
-- without removing telemetry or weakening a uniqueness constraint.
DROP INDEX IF EXISTS idx_spans_root_start;
DROP INDEX IF EXISTS idx_spans_session_start;
DROP INDEX IF EXISTS idx_spans_trace_start;
DROP INDEX IF EXISTS idx_spans_trace_id_only;

CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id);
CREATE INDEX IF NOT EXISTS idx_spans_parent_span_id ON spans(parent_span_id);
CREATE INDEX IF NOT EXISTS idx_spans_session_id ON spans(session_id);
CREATE INDEX IF NOT EXISTS idx_spans_start_ns ON spans(start_ns);
