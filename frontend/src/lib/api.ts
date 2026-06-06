import { z } from 'zod'

const BASE = ''

interface Meta { total: number; page?: number }
interface Envelope<T> { data: T; meta: Meta }

// Validate a response payload against its schema at the network boundary.
// Drift between the backend and these schemas surfaces as a loud, specific
// console error (which endpoint, which fields) instead of an `undefined.map`
// crash deep inside a component. We pass the raw value through on failure so a
// single unexpected/extra field can't blank a whole view — the schema is the
// type contract, resilience is the runtime behaviour.
function parse<S extends z.ZodTypeAny>(schema: S, data: unknown, where: string): z.infer<S> {
  const r = schema.safeParse(data)
  if (r.success) return r.data
  console.error(`[api] response validation failed: ${where}`, r.error.issues)
  return data as z.infer<S>
}

async function get<S extends z.ZodTypeAny>(path: string, schema: S): Promise<Envelope<z.infer<S>>> {
  const res = await fetch(BASE + path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const json = await res.json()
  return { data: parse(schema, json.data, `GET ${path}`), meta: json.meta }
}

async function post<S extends z.ZodTypeAny>(path: string, body: unknown, schema: S): Promise<Envelope<z.infer<S>>> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const json = await res.json()
  return { data: parse(schema, json.data, `POST ${path}`), meta: json.meta }
}

async function del<S extends z.ZodTypeAny>(path: string, schema: S): Promise<Envelope<z.infer<S>>> {
  const res = await fetch(BASE + path, { method: 'DELETE' })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const json = await res.json()
  return { data: parse(schema, json.data, `DELETE ${path}`), meta: json.meta }
}

async function patch<S extends z.ZodTypeAny>(path: string, body: unknown, schema: S): Promise<Envelope<z.infer<S>>> {
  const res = await fetch(BASE + path, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const json = await res.json()
  return { data: parse(schema, json.data, `PATCH ${path}`), meta: json.meta }
}

// ── schemas (single source of truth; types are inferred below) ────────────────
// Unknown/extra keys are stripped by default, so new backend fields are
// forward-compatible. Optional (`?`) fields use `.optional()`.

export const TraceRowSchema = z.object({
  trace_id: z.string(),
  service_name: z.string(),
  name: z.string(),
  status_code: z.number(),
  start_ns: z.number(),
  end_ns: z.number(),
  duration_ns: z.number(),
  session_id: z.string(),
  session_label: z.string(),
  has_n1: z.boolean(),
  span_count: z.number(),
  issue_kinds: z.array(z.string()).optional(),
})

export const TraceIssueSchema = z.object({
  id: z.string(),
  trace_id: z.string(),
  session_id: z.string(),
  kind: z.string(),
  fingerprint: z.string(),
  count: z.number(),
  wasted_ns: z.number(),
  parent_span_id: z.string(),
  example_span_id: z.string(),
  created_at: z.number(),
})

export const SpanEventSchema = z.object({
  span_id: z.string(),
  trace_id: z.string(),
  session_id: z.string(),
  time_ns: z.number(),
  name: z.string(),
  attributes: z.string(),
})

export const SpanLinkSchema = z.object({
  span_id: z.string(),
  trace_id: z.string(),
  session_id: z.string(),
  linked_trace_id: z.string(),
  linked_span_id: z.string(),
  trace_state: z.string(),
  attributes: z.string(),
})

export const SpanSchema = z.object({
  trace_id: z.string(),
  span_id: z.string(),
  parent_span_id: z.string(),
  service_name: z.string(),
  name: z.string(),
  kind: z.number(),
  start_ns: z.number(),
  end_ns: z.number(),
  duration_ns: z.number(),
  status_code: z.number(),
  status_message: z.string(),
  attributes: z.string(),
  resource: z.string(),
  session_id: z.string(),
  session_label: z.string(),
  received_at: z.number(),
  events: z.array(SpanEventSchema),
  links: z.array(SpanLinkSchema),
})

export const SpanRowSchema = SpanSchema.extend({
  tag: z.string().optional(),
})

export const LogSchema = z.object({
  timestamp_ns: z.number(),
  trace_id: z.string(),
  span_id: z.string(),
  severity: z.number(),
  body: z.string(),
  attributes: z.string(),
  service_name: z.string(),
  session_id: z.string(),
  received_at: z.number(),
})

export const SessionSchema = z.object({
  id: z.string(),
  label: z.string(),
  created_at: z.number(),
  is_baseline: z.boolean(),
  is_imported: z.boolean(),
  span_count: z.number(),
  trace_count: z.number(),
  services: z.string(),
  note: z.string(),
  last_activity_ns: z.number(),
  p95_ns: z.number(),
  size_bytes: z.number(),
  n1_count: z.number(),
  error_count: z.number(),
})

export const ImportResultSchema = z.object({
  session: SessionSchema,
  span_count: z.number(),
  trace_count: z.number(),
})

export const SearchResultSchema = z.object({
  kind: z.enum(['trace', 'span', 'session', 'service', 'log']),
  trace_id: z.string(),
  span_id: z.string().optional(),
  title: z.string(),
  subtitle: z.string(),
  session_id: z.string(),
})

export const MetricCatalogEntrySchema = z.object({
  name: z.string(),
  description: z.string(),
  unit: z.string(),
  type: z.enum(['gauge', 'counter', 'histogram']),
  service_name: z.string(),
  sample_count: z.number(),
})

export const MetricSeriesExemplarSchema = z.object({
  trace_id: z.string(),
  span_id: z.string(),
})

export const MetricSeriesPointSchema = z.object({
  timestamp_ns: z.number(),
  value: z.number(),
  percentile: z.enum(['p50', 'p95', 'p99']).optional(),
  exemplars: z.array(MetricSeriesExemplarSchema).optional(),
})

export const TraceOverlaySchema = z.object({
  trace_id: z.string(),
  op: z.string(),
  service: z.string(),
  status_code: z.number(),
  start_ns: z.number(),
  end_ns: z.number(),
  duration_ns: z.number(),
})

export const MetricSeriesSchema = z.object({
  name: z.string(),
  service_name: z.string(),
  type: z.enum(['gauge', 'counter', 'histogram']),
  unit: z.string(),
  description: z.string(),
  points: z.array(MetricSeriesPointSchema),
  // Populated only when ?with_traces=1 is requested. Always an array (server
  // returns [] when none) so the type stays non-optional.
  traces: z.array(TraceOverlaySchema),
})

export const CoverageRouteSchema = z.object({
  method: z.string(),
  path: z.string(),
  hits: z.number(),
  p95_ns: z.number().optional(),
})

export const ServiceCoverageSchema = z.object({
  name: z.string(),
  source: z.enum(['openapi', 'observed']),
  spec: z.string().optional(),
  observed_operations: z.number(),
  total_routes: z.number(),
  coverage_pct: z.number(),
  dark_routes: z.array(CoverageRouteSchema),
  observed_routes: z.array(CoverageRouteSchema),
})

export const CoverageReportSchema = z.object({
  services: z.array(ServiceCoverageSchema),
  overall: z.object({
    observed_operations: z.number(),
    total_routes: z.number(),
    dark_count: z.number(),
    coverage_pct: z.number(),
  }),
})

export const SettingsRuntimeSchema = z.object({
  pid: z.number(),
  uptime_ns: z.number(),
  version: z.string(),
  channel: z.string(),
  config_path: z.string(),
  otlp_grpc_port: z.number(),
  otlp_http_port: z.number(),
  db_size_bytes: z.number(),
})

export const UpdateCheckResultSchema = z.object({
  current: z.string(),
  latest: z.string(),
  channel: z.string(),
  is_outdated: z.boolean(),
  release_notes_url: z.string(),
  checked_at_ns: z.number(),
  error: z.string().optional(),
})

export const SettingsSchema = z.object({
  port: z.number(),
  db_path: z.string(),
  retention_days: z.number(),
  max_sessions: z.number(),
  max_db_size_mb: z.number(),
  otlp_grpc_port: z.number(),
  otlp_http_port: z.number(),
  no_browser: z.boolean(),
  forward: z.array(z.string()),
  bind_address_v4: z.string(),
  bind_address_v6: z.string(),
  forward_sample: z.number(),
  source_rps: z.number(),
  source_burst: z.number(),
  tls_enabled: z.boolean(),
  bearer_token_set: z.boolean(),
  self_monitor: z.boolean(),
  mcp_enabled: z.boolean(),
  mcp_allow_writes: z.boolean(),
  runtime: SettingsRuntimeSchema,
})

export const SourceStatsSchema = z.object({
  service: z.string(),
  accepted_per_sec: z.number(),
  rejected_per_sec: z.number(),
  error_rate: z.number(),
  bytes_per_sec: z.number(),
  last_seen_ns: z.number(),
})

export const ForwarderStatusSchema = z.object({
  url: z.string(),
  sent: z.number(),
  errors: z.number(),
  last_error: z.string().optional(),
  pending_bytes: z.number().optional(),
  dropped_spool: z.number().optional(),
})

export const LintWarningSchema = z.object({
  span_id: z.string(),
  trace_id: z.string(),
  session_id: z.string(),
  rule_id: z.string(),
  message: z.string(),
  severity: z.string(),
  created_at: z.number(),
})

export const StatsSchema = z.object({
  span_count: z.number(),
  trace_count: z.number(),
  log_count: z.number(),
  db_size: z.number(),
  spans_per_sec: z.number(),
  logs_per_sec: z.number(),
  metrics_per_sec: z.number(),
  peak_spans_per_sec: z.number(),
  dropped_spans: z.number(),
  dropped_logs: z.number(),
  dropped_metric_points: z.number(),
  last_drop_at: z.number(),
  storage_full: z.boolean().optional().default(false),
})

export const ServiceMapOpStatSchema = z.object({
  name: z.string(),
  count: z.number(),
  p95_ns: z.number(),
})

export const ServiceMapNodeSchema = z.object({
  id: z.string(),
  span_count: z.number(),
  error_count: z.number(),
  p95_ns: z.number(),
  top_operations: z.array(ServiceMapOpStatSchema),
})

export const ServiceMapEdgeSchema = z.object({
  from: z.string(),
  to: z.string(),
  call_count: z.number(),
  avg_duration_ns: z.number(),
  error_count: z.number(),
})

export const ServiceMapDataSchema = z.object({
  nodes: z.array(ServiceMapNodeSchema),
  edges: z.array(ServiceMapEdgeSchema),
})

export const TableStatSchema = z.object({
  name: z.string(),
  row_count: z.number(),
  approx_bytes: z.number(),
})

export const SessionSizeSchema = z.object({
  id: z.string(),
  label: z.string(),
  approx_bytes: z.number(),
  span_count: z.number(),
})

export const StorageBreakdownSchema = z.object({
  tables: z.array(TableStatSchema),
  sessions: z.array(SessionSizeSchema),
  wal_bytes: z.number(),
  main_bytes: z.number(),
  last_checkpoint_at: z.number(),
})

export const CompactResultSchema = z.object({
  bytes_before: z.number(),
  bytes_after: z.number(),
  reclaimed: z.number(),
})

// Mirrors storage.PruneResult (Go json tags).
export const PruneResultSchema = z.object({
  deleted_by_age: z.number(),
  deleted_by_count: z.number(),
  deleted_by_size: z.number(),
  final_sessions: z.number(),
  final_db_size_bytes: z.number(),
})

// Small ad-hoc response shapes.
const OkSchema = z.object({ ok: z.boolean() })
const ActiveSessionSchema = z.object({ id: z.string(), label: z.string() })

// ── inferred types (same names callers already import) ────────────────────────

export type TraceRow = z.infer<typeof TraceRowSchema>
export type TraceIssue = z.infer<typeof TraceIssueSchema>
export type SpanEvent = z.infer<typeof SpanEventSchema>
export type SpanLink = z.infer<typeof SpanLinkSchema>
export type Span = z.infer<typeof SpanSchema>
export type SpanRow = z.infer<typeof SpanRowSchema>
export type Log = z.infer<typeof LogSchema>
export type Session = z.infer<typeof SessionSchema>
export type ImportResult = z.infer<typeof ImportResultSchema>
export type SearchResult = z.infer<typeof SearchResultSchema>
export type MetricCatalogEntry = z.infer<typeof MetricCatalogEntrySchema>
export type MetricSeriesExemplar = z.infer<typeof MetricSeriesExemplarSchema>
export type MetricSeriesPoint = z.infer<typeof MetricSeriesPointSchema>
export type TraceOverlay = z.infer<typeof TraceOverlaySchema>
export type MetricSeries = z.infer<typeof MetricSeriesSchema>
export type CoverageRoute = z.infer<typeof CoverageRouteSchema>
export type ServiceCoverage = z.infer<typeof ServiceCoverageSchema>
export type CoverageReport = z.infer<typeof CoverageReportSchema>
export type SettingsRuntime = z.infer<typeof SettingsRuntimeSchema>
export type UpdateCheckResult = z.infer<typeof UpdateCheckResultSchema>
export type Settings = z.infer<typeof SettingsSchema>
export type SourceStats = z.infer<typeof SourceStatsSchema>
export type ForwarderStatus = z.infer<typeof ForwarderStatusSchema>
export type LintWarning = z.infer<typeof LintWarningSchema>
export type Stats = z.infer<typeof StatsSchema>
export type ServiceMapOpStat = z.infer<typeof ServiceMapOpStatSchema>
export type ServiceMapNode = z.infer<typeof ServiceMapNodeSchema>
export type ServiceMapEdge = z.infer<typeof ServiceMapEdgeSchema>
export type ServiceMapData = z.infer<typeof ServiceMapDataSchema>
export type TableStat = z.infer<typeof TableStatSchema>
export type SessionSize = z.infer<typeof SessionSizeSchema>
export type StorageBreakdown = z.infer<typeof StorageBreakdownSchema>
export type CompactResult = z.infer<typeof CompactResultSchema>
export type PruneResult = z.infer<typeof PruneResultSchema>

// Request payload — not a response, so no runtime validation needed.
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
  source_rps?: number
  source_burst?: number
  self_monitor?: boolean
}

export const api = {
  traces: {
    list: (sessionId?: string) =>
      get(`/api/traces${sessionId ? `?sessionId=${sessionId}` : ''}`, z.array(TraceRowSchema)),
    get: (traceId: string) => get(`/api/traces/${traceId}`, z.array(SpanSchema)),
    exportUrl: (traceId: string) => `/api/traces/${traceId}/export`,
  },
  spans: {
    list: (params?: { sort?: string; sessionId?: string; limit?: number }) => {
      const q = new URLSearchParams()
      if (params?.sort) q.set('sort', params.sort)
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.limit) q.set('limit', String(params.limit))
      const qs = q.toString()
      return get(`/api/spans${qs ? `?${qs}` : ''}`, z.array(SpanRowSchema))
    },
    get: (spanId: string) => get(`/api/spans/${spanId}`, SpanSchema),
  },
  logs: {
    list: (params?: { sessionId?: string; traceId?: string; spanId?: string }) => {
      const q = new URLSearchParams()
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.traceId) q.set('traceId', params.traceId)
      if (params?.spanId) q.set('spanId', params.spanId)
      const qs = q.toString()
      return get(`/api/logs${qs ? `?${qs}` : ''}`, z.array(LogSchema))
    },
  },
  services: {
    list: () => get('/api/services', z.array(z.string())),
  },
  sessions: {
    list: () => get('/api/sessions', z.array(SessionSchema)),
    get: (id: string) => get(`/api/sessions/${id}`, SessionSchema),
    getActive: () => get('/api/sessions/active', ActiveSessionSchema),
    create: (label?: string) => post('/api/sessions', { label }, SessionSchema),
    activate: (id: string) => post(`/api/sessions/${id}/activate`, {}, SessionSchema),
    baseline: (id: string, isBaseline: boolean) =>
      post(`/api/sessions/${id}/baseline`, { is_baseline: isBaseline }, OkSchema),
    patch: (id: string, body: { label?: string; note?: string }) =>
      patch(`/api/sessions/${id}`, body, SessionSchema),
    delete: (id: string) => del(`/api/sessions/${id}`, OkSchema),
    import: async (label: string, format: string, data: string): Promise<Envelope<ImportResult>> => {
      const q = new URLSearchParams({ label, format })
      const r = await fetch(`/api/sessions/import?${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: data,
      })
      if (!r.ok) throw new Error(await r.text())
      const json = await r.json()
      return { data: parse(ImportResultSchema, json.data, 'POST /api/sessions/import'), meta: json.meta }
    },
  },
  lint: {
    list: (sessionId?: string) =>
      get(`/api/lint${sessionId ? `?sessionId=${sessionId}` : ''}`, z.array(LintWarningSchema)),
  },
  stats: {
    get: (sessionId?: string) =>
      get(`/api/stats${sessionId ? `?sessionId=${sessionId}` : ''}`, StatsSchema),
  },
  serviceMap: {
    get: (sessionId?: string) =>
      get(`/api/service-map${sessionId ? `?sessionId=${sessionId}` : ''}`, ServiceMapDataSchema),
  },
  issues: {
    get: (traceId: string) => get(`/api/issues?traceId=${traceId}`, z.array(TraceIssueSchema)),
    list: (sessionId?: string) =>
      get(`/api/issues${sessionId ? `?sessionId=${sessionId}` : ''}`, z.array(TraceIssueSchema)),
  },
  health: {
    get: () => get('/api/health', OkSchema),
    // Presents the bearer token to /api/health so the server sets the auth cookie.
    seed: (token: string): Promise<void> =>
      fetch('/api/health', { headers: { Authorization: `Bearer ${token}` } }).then(() => undefined),
  },
  sources: {
    list: () => get('/api/sources', z.array(SourceStatsSchema)),
  },
  forwarders: {
    list: () => get('/api/forwarders', z.array(ForwarderStatusSchema)),
  },
  settings: {
    get: () => get('/api/settings', SettingsSchema),
    update: async (patchBody: SettingsUpdate): Promise<Envelope<Settings>> => {
      const r = await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patchBody),
      })
      if (!r.ok) throw new Error(await r.text())
      const json = await r.json()
      return { data: parse(SettingsSchema, json.data, 'PUT /api/settings'), meta: json.meta }
    },
    dropAllData: async () => {
      const r = await fetch('/api/settings/data', { method: 'DELETE' })
      if (!r.ok) throw new Error(await r.text())
      return r.json()
    },
    compact: async (): Promise<Envelope<CompactResult>> => {
      const r = await fetch('/api/settings/compact', { method: 'POST' })
      if (!r.ok) throw new Error(await r.text())
      const json = await r.json()
      return { data: parse(CompactResultSchema, json.data, 'POST /api/settings/compact'), meta: json.meta }
    },
    prune: async (): Promise<Envelope<PruneResult>> => {
      const r = await fetch('/api/settings/prune', { method: 'POST' })
      if (!r.ok) throw new Error(await r.text())
      const json = await r.json()
      return { data: parse(PruneResultSchema, json.data, 'POST /api/settings/prune'), meta: json.meta }
    },
    checkUpdates: () => post('/api/settings/check-updates', {}, UpdateCheckResultSchema),
  },
  storage: {
    get: () => get('/api/storage', StorageBreakdownSchema),
  },
  coverage: {
    get: (sessionId?: string) =>
      get(`/api/coverage${sessionId ? `?sessionId=${sessionId}` : ''}`, CoverageReportSchema),
  },
  metrics: {
    list: (sessionId?: string) =>
      get(`/api/metrics${sessionId ? `?sessionId=${sessionId}` : ''}`, z.array(MetricCatalogEntrySchema)),
    series: (params: { name: string; service?: string; sessionId?: string; from?: number; to?: number; withTraces?: boolean }) => {
      const q = new URLSearchParams({ name: params.name })
      if (params.service) q.set('service', params.service)
      if (params.sessionId) q.set('sessionId', params.sessionId)
      if (params.from) q.set('from', String(params.from))
      if (params.to) q.set('to', String(params.to))
      if (params.withTraces) q.set('with_traces', '1')
      return get(`/api/metrics/series?${q}`, MetricSeriesSchema)
    },
  },
  search: {
    query: (q: string, sessionId?: string) => {
      const params = new URLSearchParams({ q, limit: '20' })
      if (sessionId) params.set('sessionId', sessionId)
      return get(`/api/search?${params}`, z.array(SearchResultSchema))
    },
  },
}
