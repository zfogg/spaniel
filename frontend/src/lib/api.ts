const BASE = ''

async function get<T>(path: string): Promise<{ data: T; meta: { total: number; page: number } }> {
  const res = await fetch(BASE + path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

async function post<T>(path: string, body: unknown): Promise<{ data: T; meta: { total: number } }> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

async function del<T>(path: string): Promise<{ data: T; meta: { total: number } }> {
  const res = await fetch(BASE + path, { method: 'DELETE' })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

async function patch<T>(path: string, body: unknown): Promise<{ data: T; meta: { total: number } }> {
  const res = await fetch(BASE + path, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export interface TraceRow {
  trace_id: string
  service_name: string
  name: string
  status_code: number
  start_ns: number
  end_ns: number
  duration_ns: number
  session_id: string
  session_label: string
  has_n1: boolean
  span_count: number
  issue_kinds: string[]
}

export interface TraceIssue {
  id: string
  trace_id: string
  session_id: string
  kind: string
  fingerprint: string
  count: number
  wasted_ns: number
  parent_span_id: string
  example_span_id: string
  created_at: number
}

export interface SpanEvent {
  span_id: string
  trace_id: string
  session_id: string
  time_ns: number
  name: string
  attributes: string
}

export interface SpanLink {
  span_id: string
  trace_id: string
  session_id: string
  linked_trace_id: string
  linked_span_id: string
  trace_state: string
  attributes: string
}

export interface Span {
  trace_id: string
  span_id: string
  parent_span_id: string
  service_name: string
  name: string
  kind: number
  start_ns: number
  end_ns: number
  duration_ns: number
  status_code: number
  status_message: string
  attributes: string
  resource: string
  session_id: string
  session_label: string
  received_at: number
  events: SpanEvent[]
  links: SpanLink[]
}

export interface SpanRow extends Span {
  tag?: string
}

export interface Log {
  timestamp_ns: number
  trace_id: string
  span_id: string
  severity: number
  body: string
  attributes: string
  service_name: string
  session_id: string
  received_at: number
}

export interface Session {
  id: string
  label: string
  created_at: number
  is_baseline: boolean
  is_imported: boolean
  span_count: number
  trace_count: number
  services: string
  note: string
  last_activity_ns: number
  p95_ns: number
  size_bytes: number
  n1_count: number
  error_count: number
}

export interface ImportResult {
  session: Session
  span_count: number
  trace_count: number
}

export interface SearchResult {
  kind: 'trace' | 'span' | 'session' | 'service' | 'log'
  trace_id: string
  span_id?: string
  title: string
  subtitle: string
  session_id: string
}

export interface MetricCatalogEntry {
  name: string
  description: string
  unit: string
  type: 'gauge' | 'counter' | 'histogram'
  service_name: string
  sample_count: number
}

export interface MetricSeriesPoint {
  timestamp_ns: number
  value: number
  percentile?: 'p50' | 'p95' | 'p99'
}

export interface TraceOverlay {
  trace_id: string
  op: string
  service: string
  status_code: number
  start_ns: number
  end_ns: number
  duration_ns: number
}

export interface MetricSeries {
  name: string
  service_name: string
  type: 'gauge' | 'counter' | 'histogram'
  unit: string
  description: string
  points: MetricSeriesPoint[]
  // Populated only when ?with_traces=1 is requested. Always an array
  // (server returns [] when none) so the type stays non-optional.
  traces: TraceOverlay[]
}

export interface CoverageRoute {
  method: string
  path: string
  hits: number
  p95_ns?: number
}

export interface ServiceCoverage {
  name: string
  source: 'openapi' | 'observed'
  spec?: string
  observed_operations: number
  total_routes: number
  coverage_pct: number
  dark_routes: CoverageRoute[]
  observed_routes: CoverageRoute[]
}

export interface CoverageReport {
  services: ServiceCoverage[]
  overall: {
    observed_operations: number
    total_routes: number
    dark_count: number
    coverage_pct: number
  }
}

export interface SettingsRuntime {
  pid: number
  uptime_ns: number
  version: string
  config_path: string
  otlp_grpc_port: number
  otlp_http_port: number
  db_size_bytes: number
}

export interface Settings {
  port: number
  db_path: string
  retention_days: number
  max_sessions: number
  max_db_size_mb: number
  otlp_grpc_port: number
  otlp_http_port: number
  no_browser: boolean
  forward: string[]
  bind_address_v4: string
  bind_address_v6: string
  forward_sample: number
  tls_enabled: boolean
  bearer_token_set: boolean
  runtime: SettingsRuntime
}

export interface SettingsUpdate {
  port?: number
  db_path?: string
  retention_days?: number
  max_sessions?: number
  max_db_size_mb?: number
  otlp_grpc_port?: number
  otlp_http_port?: number
  no_browser?: boolean
  forward?: string[]
  bind_address_v4?: string
  bind_address_v6?: string
  forward_sample?: number
}

export interface SourceStats {
  service: string
  accepted_per_sec: number
  rejected_per_sec: number
  error_rate: number
  bytes_per_sec: number
  last_seen_ns: number
}

export interface ForwarderStatus {
  url: string
  sent: number
  errors: number
  last_error?: string
  pending_bytes?: number
  dropped_spool?: number
}

export interface LintWarning {
  span_id: string
  trace_id: string
  session_id: string
  rule_id: string
  message: string
  severity: string
  created_at: number
}

export interface Stats {
  span_count: number
  trace_count: number
  log_count: number
  db_size: number
  spans_per_sec: number
  logs_per_sec: number
  metrics_per_sec: number
  peak_spans_per_sec: number
  dropped_spans: number
  dropped_logs: number
  dropped_metric_points: number
  last_drop_at: number
}

export interface ServiceMapOpStat {
  name: string
  count: number
  p95_ns: number
}

export interface ServiceMapNode {
  id: string
  span_count: number
  error_count: number
  p95_ns: number
  top_operations: ServiceMapOpStat[]
}

export interface ServiceMapEdge {
  from: string
  to: string
  call_count: number
  avg_duration_ns: number
  error_count: number
}

export interface ServiceMapData {
  nodes: ServiceMapNode[]
  edges: ServiceMapEdge[]
}

export interface TableStat {
  name: string
  row_count: number
  approx_bytes: number
}

export interface SessionSize {
  id: string
  label: string
  approx_bytes: number
  span_count: number
}

export interface StorageBreakdown {
  tables: TableStat[]
  sessions: SessionSize[]
  wal_bytes: number
  main_bytes: number
  last_checkpoint_at: number
}

export interface CompactResult {
  bytes_before: number
  bytes_after: number
  reclaimed: number
}

// Mirrors storage.PruneResult (Go json tags).
export interface PruneResult {
  deleted_by_age: number
  deleted_by_count: number
  deleted_by_size: number
  final_sessions: number
  final_db_size_bytes: number
}

export const api = {
  traces: {
    list: (sessionId?: string) =>
      get<TraceRow[]>(`/api/traces${sessionId ? `?sessionId=${sessionId}` : ''}`),
    get: (traceId: string) => get<Span[]>(`/api/traces/${traceId}`),
    exportUrl: (traceId: string) => `/api/traces/${traceId}/export`,
  },
  spans: {
    list: (params?: { sort?: string; sessionId?: string; limit?: number }) => {
      const q = new URLSearchParams()
      if (params?.sort) q.set('sort', params.sort)
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.limit) q.set('limit', String(params.limit))
      const qs = q.toString()
      return get<SpanRow[]>(`/api/spans${qs ? `?${qs}` : ''}`)
    },
    get: (spanId: string) => get<Span>(`/api/spans/${spanId}`),
  },
  logs: {
    list: (params?: { sessionId?: string; traceId?: string; spanId?: string }) => {
      const q = new URLSearchParams()
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.traceId) q.set('traceId', params.traceId)
      if (params?.spanId) q.set('spanId', params.spanId)
      const qs = q.toString()
      return get<Log[]>(`/api/logs${qs ? `?${qs}` : ''}`)
    },
  },
  services: {
    list: () => get<string[]>('/api/services'),
  },
  sessions: {
    list: () => get<Session[]>('/api/sessions'),
    get: (id: string) => get<Session>(`/api/sessions/${id}`),
    getActive: () => get<{ id: string; label: string }>('/api/sessions/active'),
    create: (label?: string) => post<Session>('/api/sessions', { label }),
    activate: (id: string) => post<Session>(`/api/sessions/${id}/activate`, {}),
    baseline: (id: string, isBaseline: boolean) =>
      post<{ ok: boolean }>(`/api/sessions/${id}/baseline`, { is_baseline: isBaseline }),
    patch: (id: string, body: { label?: string; note?: string }) =>
      patch<Session>(`/api/sessions/${id}`, body),
    delete: (id: string) => del<{ ok: boolean }>(`/api/sessions/${id}`),
    import: (label: string, format: string, data: string) => {
      const q = new URLSearchParams({ label, format })
      return fetch(`/api/sessions/import?${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: data,
      }).then(r => {
        if (!r.ok) return r.text().then(t => { throw new Error(t) })
        return r.json() as Promise<{ data: ImportResult; meta: { total: number; page: number } }>
      })
    },
  },
  lint: {
    list: (sessionId?: string) =>
      get<LintWarning[]>(`/api/lint${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  stats: {
    get: (sessionId?: string) =>
      get<Stats>(`/api/stats${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  serviceMap: {
    get: (sessionId?: string) =>
      get<ServiceMapData>(`/api/service-map${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  issues: {
    get: (traceId: string) =>
      get<TraceIssue[]>(`/api/issues?traceId=${traceId}`),
    list: (sessionId?: string) =>
      get<TraceIssue[]>(`/api/issues${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  health: {
    get: () => get<{ ok: boolean }>('/api/health'),
    // Presents the bearer token to /api/health so the server sets the auth cookie.
    seed: (token: string): Promise<void> =>
      fetch('/api/health', { headers: { Authorization: `Bearer ${token}` } }).then(() => undefined),
  },
  sources: {
    list: () => get<SourceStats[]>('/api/sources'),
  },
  forwarders: {
    list: () => get<ForwarderStatus[]>('/api/forwarders'),
  },
  settings: {
    get: () => get<Settings>('/api/settings'),
    update: (patch: SettingsUpdate) =>
      fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      }).then(r => {
        if (!r.ok) return r.text().then(t => { throw new Error(t) })
        return r.json() as Promise<{ data: Settings; meta: { total: number; page: number } }>
      }),
    dropAllData: () =>
      fetch('/api/settings/data', { method: 'DELETE' }).then(r => {
        if (!r.ok) return r.text().then(t => { throw new Error(t) })
        return r.json()
      }),
    compact: () =>
      fetch('/api/settings/compact', { method: 'POST' }).then(r => {
        if (!r.ok) return r.text().then(t => { throw new Error(t) })
        return r.json() as Promise<{ data: CompactResult; meta: { total: number; page: number } }>
      }),
    prune: () =>
      fetch('/api/settings/prune', { method: 'POST' }).then(r => {
        if (!r.ok) return r.text().then(t => { throw new Error(t) })
        return r.json() as Promise<{ data: PruneResult; meta: { total: number; page: number } }>
      }),
  },
  storage: {
    get: () => get<StorageBreakdown>('/api/storage'),
  },
  coverage: {
    get: (sessionId?: string) =>
      get<CoverageReport>(`/api/coverage${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  metrics: {
    list: (sessionId?: string) =>
      get<MetricCatalogEntry[]>(`/api/metrics${sessionId ? `?sessionId=${sessionId}` : ''}`),
    series: (params: { name: string; service?: string; sessionId?: string; from?: number; to?: number; withTraces?: boolean }) => {
      const q = new URLSearchParams({ name: params.name })
      if (params.service) q.set('service', params.service)
      if (params.sessionId) q.set('sessionId', params.sessionId)
      if (params.from) q.set('from', String(params.from))
      if (params.to) q.set('to', String(params.to))
      if (params.withTraces) q.set('with_traces', '1')
      return get<MetricSeries>(`/api/metrics/series?${q}`)
    },
  },
  search: {
    query: (q: string, sessionId?: string) => {
      const params = new URLSearchParams({ q, limit: '20' })
      if (sessionId) params.set('sessionId', sessionId)
      return get<SearchResult[]>(`/api/search?${params}`)
    },
  },
}
