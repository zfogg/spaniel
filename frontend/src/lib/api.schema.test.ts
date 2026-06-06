import { describe, it, expect } from 'vitest'
import {
  TraceRowSchema, SpanSchema, LogSchema, SessionSchema, SettingsSchema,
  ServiceMapDataSchema, MetricSeriesSchema, MetricCatalogEntrySchema,
  CoverageReportSchema, StatsSchema, LintWarningSchema, TraceIssueSchema,
} from './api'

// Representative payloads mirroring the Go json contract. These guard against
// schema drift: if a schema diverges from the shape the backend actually sends
// (or from the api.ts type contract), these fail — unlike the runtime path,
// which warns-and-passes for resilience.

describe('api response schemas', () => {
  it('accepts a full TraceRow', () => {
    expect(TraceRowSchema.safeParse({
      trace_id: 'abc', service_name: 'api', name: 'GET /x', status_code: 0,
      start_ns: 1, end_ns: 2, duration_ns: 1, session_id: 's1', session_label: 'main',
      has_n1: false, span_count: 3, issue_kinds: ['slow_db'],
    }).success).toBe(true)
  })

  it('accepts a TraceRow without the optional issue_kinds', () => {
    expect(TraceRowSchema.safeParse({
      trace_id: 'abc', service_name: 'api', name: 'GET /x', status_code: 0,
      start_ns: 1, end_ns: 2, duration_ns: 1, session_id: 's1', session_label: 'main',
      has_n1: false, span_count: 3,
    }).success).toBe(true)
  })

  it('accepts a Span with nested events and links', () => {
    expect(SpanSchema.safeParse({
      trace_id: 't', span_id: 's', parent_span_id: '', service_name: 'api', name: 'op',
      kind: 2, start_ns: 1, end_ns: 2, duration_ns: 1, status_code: 0, status_message: '',
      attributes: '{}', resource: '{}', session_id: 's1', session_label: 'main', received_at: 5,
      events: [{ span_id: 's', trace_id: 't', session_id: 's1', time_ns: 1, name: 'ev', attributes: '{}' }],
      links: [{ span_id: 's', trace_id: 't', session_id: 's1', linked_trace_id: 'lt', linked_span_id: 'ls', trace_state: '', attributes: '{}' }],
    }).success).toBe(true)
  })

  it('accepts a Log', () => {
    expect(LogSchema.safeParse({
      timestamp_ns: 1, trace_id: 't', span_id: 's', severity: 9, body: 'hi',
      attributes: '{}', service_name: 'api', session_id: 's1', received_at: 1,
    }).success).toBe(true)
  })

  it('accepts a Session', () => {
    expect(SessionSchema.safeParse({
      id: 's1', label: 'main', created_at: 1, is_baseline: false, is_imported: false,
      span_count: 1, trace_count: 1, services: 'api', note: '', last_activity_ns: 1,
      p95_ns: 1, size_bytes: 1, n1_count: 0, error_count: 0,
    }).success).toBe(true)
  })

  it('accepts Settings with nested runtime', () => {
    expect(SettingsSchema.safeParse({
      port: 8080, db_path: '/db', retention_days: 7, max_sessions: 50, max_db_size_mb: 500,
      otlp_grpc_port: 4317, otlp_http_port: 4318, no_browser: false, forward: ['http://x'],
      bind_address_v4: '127.0.0.1', bind_address_v6: '::1', forward_sample: 1, source_rps: 100,
      source_burst: 50, tls_enabled: false, bearer_token_set: false, self_monitor: true,
      mcp_enabled: true, mcp_allow_writes: false,
      runtime: { pid: 1, uptime_ns: 1, version: 'v0', channel: 'stable', config_path: '/c',
        otlp_grpc_port: 4317, otlp_http_port: 4318, db_size_bytes: 1 },
    }).success).toBe(true)
  })

  it('accepts ServiceMapData, MetricSeries, Coverage, Stats, Lint, Issue shapes', () => {
    expect(ServiceMapDataSchema.safeParse({
      nodes: [{ id: 'api', span_count: 1, error_count: 0, p95_ns: 1, top_operations: [{ name: 'op', count: 1, p95_ns: 1 }] }],
      edges: [{ from: 'api', to: 'db', call_count: 1, avg_duration_ns: 1, error_count: 0 }],
    }).success).toBe(true)

    expect(MetricCatalogEntrySchema.safeParse({
      name: 'm', description: '', unit: 'ms', type: 'histogram', service_name: 'api', sample_count: 1,
    }).success).toBe(true)

    expect(MetricSeriesSchema.safeParse({
      name: 'm', service_name: 'api', type: 'gauge', unit: 'ms', description: '',
      points: [{ timestamp_ns: 1, value: 2, percentile: 'p95', exemplars: [{ trace_id: 't', span_id: 's' }] }],
      traces: [],
    }).success).toBe(true)

    expect(CoverageReportSchema.safeParse({
      services: [{ name: 'api', source: 'observed', observed_operations: 1, total_routes: 2,
        coverage_pct: 50, dark_routes: [{ method: 'GET', path: '/x', hits: 0 }],
        observed_routes: [{ method: 'GET', path: '/y', hits: 5, p95_ns: 10 }] }],
      overall: { observed_operations: 1, total_routes: 2, dark_count: 1, coverage_pct: 50 },
    }).success).toBe(true)

    expect(StatsSchema.safeParse({
      span_count: 1, trace_count: 1, log_count: 1, db_size: 1, spans_per_sec: 0, logs_per_sec: 0,
      metrics_per_sec: 0, peak_spans_per_sec: 0, dropped_spans: 0, dropped_logs: 0,
      dropped_metric_points: 0, last_drop_at: 0,
    }).success).toBe(true)

    expect(LintWarningSchema.safeParse({
      span_id: 's', trace_id: 't', session_id: 's1', rule_id: 'db.missing_system',
      message: 'x', severity: 'error', created_at: 1,
    }).success).toBe(true)

    expect(TraceIssueSchema.safeParse({
      id: 'i', trace_id: 't', session_id: 's1', kind: 'n_plus_one', fingerprint: 'fp',
      count: 47, wasted_ns: 1, parent_span_id: 'p', example_span_id: 'e', created_at: 1,
    }).success).toBe(true)
  })

  it('rejects payloads with a missing required field or wrong type', () => {
    // missing span_count
    expect(TraceRowSchema.safeParse({
      trace_id: 'abc', service_name: 'api', name: 'GET /x', status_code: 0,
      start_ns: 1, end_ns: 2, duration_ns: 1, session_id: 's1', session_label: 'main', has_n1: false,
    }).success).toBe(false)
    // wrong type for duration_ns
    expect(SpanSchema.safeParse({
      trace_id: 't', span_id: 's', parent_span_id: '', service_name: 'api', name: 'op',
      kind: 2, start_ns: 1, end_ns: 2, duration_ns: 'nope', status_code: 0, status_message: '',
      attributes: '{}', resource: '{}', session_id: 's1', session_label: 'main', received_at: 5,
      events: [], links: [],
    }).success).toBe(false)
    // invalid enum for metric type
    expect(MetricCatalogEntrySchema.safeParse({
      name: 'm', description: '', unit: 'ms', type: 'summary', service_name: 'api', sample_count: 1,
    }).success).toBe(false)
  })
})
