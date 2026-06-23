-- Performance indexes for the spans table
-- These dramatically speed up ListTraces and GetTrace queries by avoiding full table scans

-- Core lookup indexes
CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id);
CREATE INDEX IF NOT EXISTS idx_spans_parent_span_id ON spans(parent_span_id);
CREATE INDEX IF NOT EXISTS idx_spans_session_id ON spans(session_id);
CREATE INDEX IF NOT EXISTS idx_spans_start_ns ON spans(start_ns);

-- Composite index for the most common query pattern: root spans ordered by time
-- This covers: WHERE (parent_span_id = '' OR parent_span_id IS NULL) ORDER BY start_ns DESC
CREATE INDEX IF NOT EXISTS idx_spans_root_start ON spans(parent_span_id, start_ns);

-- Composite index for session-scoped trace queries
CREATE INDEX IF NOT EXISTS idx_spans_session_start ON spans(session_id, start_ns);

-- Composite index for trace_id lookups with span ordering
CREATE INDEX IF NOT EXISTS idx_spans_trace_start ON spans(trace_id, start_ns);

-- Index for trace_issues to speed up the LEFT JOIN in ListTraces
CREATE INDEX IF NOT EXISTS idx_trace_issues_trace_id ON trace_issues(trace_id);

-- Index for span count subquery optimization
CREATE INDEX IF NOT EXISTS idx_spans_trace_id_only ON spans(trace_id);
