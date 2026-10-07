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

async function get<S extends z.ZodTypeAny>(path: string, schema: S, signal?: AbortSignal): Promise<Envelope<z.infer<S>>> {
  const res = await fetch(BASE + path, { signal })
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
  if (!res.ok) {
    const detail = await res.json().catch(() => null)
    throw new Error(typeof detail?.error === 'string' ? detail.error : `${res.status} ${res.statusText}`)
  }
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
  attributes: z.string(),
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

export const SpanGroupSchema = z.object({
  service_name: z.string(), name: z.string(), kind: z.number(), count: z.number(),
  latest_start_ns: z.number(), error_count: z.number(), p50_duration_ns: z.number(),
  p95_duration_ns: z.number(), max_duration_ns: z.number(), attribute_variants: z.number(),
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
  type: z.string(),
	aggregation_temporality: z.string().optional(),
	is_monotonic: z.boolean().optional(),
  service_name: z.string(),
  sample_count: z.number(),
  last_timestamp_ns: z.number(),
})

export const MetricSeriesExemplarSchema = z.object({
  trace_id: z.string(),
  span_id: z.string(),
})

export const MetricSeriesPointSchema = z.object({
	start_timestamp_ns: z.number().optional(),
  timestamp_ns: z.number(),
	flags: z.number().optional(),
  value: z.number(),
	percentile: z.enum(['p50', 'p95', 'p99']).optional(),
	count: z.number().optional(),
	sum: z.number().optional(),
	min: z.number().optional(),
	max: z.number().optional(),
	bounds: z.array(z.number()).optional(),
	buckets: z.array(z.number()).optional(),
	quantiles: z.record(z.string(), z.number()).optional(),
	  exp_scale: z.number().optional(),
	  exp_zero_count: z.number().optional(),
	  exp_zero_threshold: z.number().optional(),
	  exp_positive_offset: z.number().optional(),
	  exp_positive_counts: z.array(z.number()).optional(),
	  exp_negative_offset: z.number().optional(),
	  exp_negative_counts: z.array(z.number()).optional(),
	  scope_name: z.string().optional(),
	  scope_version: z.string().optional(),
	  scope_schema_url: z.string().optional(),
	  scope_attributes: z.record(z.string(), z.unknown()).optional(),
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
  type: z.string(),
  unit: z.string(),
  description: z.string(),
	aggregation_temporality: z.string().optional(),
	is_monotonic: z.boolean().optional(),
	operation: z.string().optional(),
  points: z.array(MetricSeriesPointSchema),
	series: z.array(z.object({ key: z.string(), attributes: z.record(z.string(), z.unknown()), points: z.array(MetricSeriesPointSchema) })).optional(),
  dimensions: z.record(z.string(), z.array(z.string())).optional(),
  aggregation: z.string().optional(),
  // Populated only when ?with_traces=1 is requested. Always an array (server
  // returns [] when none) so the type stays non-optional.
  traces: z.array(TraceOverlaySchema),
})

export const MetricCardinalityStreamSchema = z.object({
	service_name: z.string(),
	name: z.string(),
	active_series: z.number(),
	limit: z.number(),
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
  auto_prune: z.boolean(),
  advance_session_on_start: z.boolean(),
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

// Older Spaniel daemons predate auto_prune. Keep the form schema strict while
// accepting their read responses with the safe default retention policy.
export const SettingsResponseSchema = SettingsSchema.extend({
  auto_prune: z.boolean().default(true),
  advance_session_on_start: z.boolean().default(true),
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
  // v0.2.2 emitted null when no session sizes were available.
  sessions: z.array(SessionSizeSchema).nullish().transform(value => value ?? []),
  wal_bytes: z.number(),
  main_bytes: z.number(),
  last_checkpoint_at: z.number(),
})

export const DashboardVariableSchema = z.object({ dashboard_id: z.string(), name: z.string(), kind: z.enum(['attribute', 'string', 'number', 'boolean', 'duration', 'time', 'enum', 'service', 'operation', 'trace_id', 'span_id', 'log_id']), source: z.string(), options_json: z.string(), default_value: z.string() })
export const DashboardPanelSchema = z.object({ id: z.string(), dashboard_id: z.string(), title: z.string(), display_type: z.enum(['single_value', 'time_series', 'table', 'heatmap', 'entity_list', 'trace_list', 'span_list', 'log_list', 'deploy_correlation']), query_sql: z.string(), query_version: z.number(), settings_json: z.string(), layout_json: z.string(), position: z.number(), updated_at: z.number() })
export const DashboardSchema = z.object({ id: z.string(), name: z.string(), description: z.string(), created_at: z.number(), updated_at: z.number(), variables: z.array(DashboardVariableSchema).default([]), panels: z.array(DashboardPanelSchema).default([]) })
export const QueryPreviewSchema = z.object({ display_type: z.string().optional(), columns: z.array(z.string()), rows: z.array(z.record(z.string(), z.unknown())), warnings: z.array(z.string()).default([]) })
export const QueryCatalogEntrySchema = z.object({ signal: z.string(), name: z.string(), query: z.string(), display_type: z.string(), attributes: z.record(z.string(), z.unknown()).optional() })
export const AlertInstanceSchema = z.object({ rule_id: z.string(), group_key: z.string(), labels_json: z.string(), state: z.string(), value: z.number().nullable().optional(), first_pending_at: z.number().nullable().optional(), fired_at: z.number().nullable().optional(), resolved_at: z.number().nullable().optional(), acknowledged_at: z.number().nullable().optional(), last_evaluated_at: z.number(), last_error: z.string() })
export const AlertRuleSchema = z.object({ id: z.string(), name: z.string(), query_sql: z.string(), query_version: z.number(), condition_json: z.string(), group_by_json: z.string(), annotations_json: z.string(), pending_for_ns: z.number(), cooldown_ns: z.number(), severity: z.enum(['info', 'warning', 'critical']), enabled: z.boolean(), created_at: z.number(), updated_at: z.number(), instances: z.array(AlertInstanceSchema).default([]) })

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
export type SpanGroup = z.infer<typeof SpanGroupSchema>
export type Log = z.infer<typeof LogSchema>
export type Session = z.infer<typeof SessionSchema>
export type ImportResult = z.infer<typeof ImportResultSchema>
export type SearchResult = z.infer<typeof SearchResultSchema>
export type MetricCatalogEntry = z.infer<typeof MetricCatalogEntrySchema>
export type MetricSeriesExemplar = z.infer<typeof MetricSeriesExemplarSchema>
export type MetricSeriesPoint = z.infer<typeof MetricSeriesPointSchema>
export type TraceOverlay = z.infer<typeof TraceOverlaySchema>
export type MetricSeries = z.infer<typeof MetricSeriesSchema>
export type MetricCardinalityStream = z.infer<typeof MetricCardinalityStreamSchema>
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
export type Dashboard = z.infer<typeof DashboardSchema>
export type DashboardPanel = z.infer<typeof DashboardPanelSchema>
export type DashboardVariable = z.infer<typeof DashboardVariableSchema>
export type QueryPreview = z.infer<typeof QueryPreviewSchema>
export type QueryCatalogEntry = z.infer<typeof QueryCatalogEntrySchema>
export type AlertRule = z.infer<typeof AlertRuleSchema>

// Request payload — not a response, so no runtime validation needed.
export interface SettingsUpdate {
  port?: number
  db_path?: string
  retention_days?: number
  max_sessions?: number
  max_db_size_mb?: number
  auto_prune?: boolean
  advance_session_on_start?: boolean
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
	 dashboards: {
		reorder: (ids: string[]) => post('/api/dashboards/reorder', { ids }, OkSchema),
		list: () => get('/api/dashboards', z.array(DashboardSchema)),
		get: (id: string) => get(`/api/dashboards/${id}`, DashboardSchema),
		create: (body: { name: string; description?: string; panels?: Array<Pick<DashboardPanel, 'title' | 'display_type' | 'query_sql'>> }) => post('/api/dashboards', body, DashboardSchema),
		update: (id: string, body: { name: string; description?: string }) => patch(`/api/dashboards/${id}`, body, DashboardSchema),
		remove: (id: string) => del(`/api/dashboards/${id}`, OkSchema),
		config: async (id: string) => {
			const response = await fetch(`/api/dashboards/${id}/config`)
			if (!response.ok) throw new Error(await response.text())
			return response.text()
		},
				preview: (id: string, body: { query_sql: string; name?: string; display_type?: string; variables?: Record<string, string> }) => post(`/api/dashboards/${id}/query-preview`, body, QueryPreviewSchema),
                catalog: (signal?: string, search?: string, abortSignal?: AbortSignal) => {
                  const query = new URLSearchParams()
                  if (signal) query.set('signal', signal)
                  if (search) query.set('q', search)
                  return get(`/api/query-catalog${query.size ? `?${query}` : ''}`, z.array(QueryCatalogEntrySchema), abortSignal)
                },
                panel: (id: string, body: { title: string; display_type: string; query_sql: string; settings_json?: string; layout_json?: string; position: number }) => post(`/api/dashboards/${id}/panels`, body, DashboardPanelSchema),
                updatePanel: (id: string, panelId: string, body: { title: string; display_type: string; query_sql: string; settings_json?: string; layout_json?: string; position: number }) => patch(`/api/dashboards/${id}/panels/${panelId}`, body, DashboardPanelSchema),
		removePanel: (id: string, panelId: string) => del(`/api/dashboards/${id}/panels/${panelId}`, OkSchema),
		movePanel: (id: string, panelId: string, direction: -1 | 1) => post(`/api/dashboards/${id}/panels/${panelId}/move`, { direction }, OkSchema),
		variable: (id: string, body: { name: string; kind: string; source: string; options_json?: string; default_value?: string }) => post(`/api/dashboards/${id}/variables`, body, DashboardVariableSchema),
		deleteVariable: (id: string, name: string) => del(`/api/dashboards/${id}/variables/${encodeURIComponent(name)}`, OkSchema),
	 },
	 alerts: {
		list: () => get('/api/alerts', z.array(AlertRuleSchema)),
		create: (body: Record<string, unknown>) => post('/api/alerts', body, AlertRuleSchema),
		update: (id: string, body: Record<string, unknown>) => patch(`/api/alerts/${id}`, body, AlertRuleSchema),
		acknowledge: (id: string) => post(`/api/alerts/${id}/acknowledge`, {}, OkSchema),
		preview: (id: string) => post(`/api/alerts/${id}/preview`, {}, QueryPreviewSchema),
	 },
  traces: {
    list: (params: { sessionId?: string; service?: string; page?: number; limit?: number } = {}) => {
      const search = new URLSearchParams()
      if (params.sessionId) search.set('sessionId', params.sessionId)
      if (params.service) search.set('service', params.service)
      if (params.page) search.set('page', String(params.page))
      if (params.limit) search.set('limit', String(params.limit))
      const query = search.toString()
      return get(`/api/traces${query ? `?${query}` : ''}`, z.array(TraceRowSchema))
    },
    get: (traceId: string) => get(`/api/traces/${traceId}`, z.array(SpanSchema)),
    exportUrl: (traceId: string) => `/api/traces/${traceId}/export`,
  },
  spans: {
    list: (params?: { sort?: string; sessionId?: string; limit?: number; page?: number; service?: string; name?: string; kind?: number }) => {
      const q = new URLSearchParams()
      if (params?.sort) q.set('sort', params.sort)
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.limit) q.set('limit', String(params.limit))
      if (params?.page) q.set('page', String(params.page))
      if (params?.service) q.set('service', params.service)
      if (params?.name) q.set('name', params.name)
      if (params?.kind !== undefined) q.set('kind', String(params.kind))
      const qs = q.toString()
      return get(`/api/spans${qs ? `?${qs}` : ''}`, z.array(SpanRowSchema))
    },
    groups: (params?: { sessionId?: string; limit?: number; page?: number }) => {
      const q = new URLSearchParams({ view: 'grouped' })
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.limit) q.set('limit', String(params.limit))
      if (params?.page) q.set('page', String(params.page))
      return get(`/api/spans?${q}`, z.array(SpanGroupSchema))
    },
    get: (spanId: string) => get(`/api/spans/${spanId}`, SpanSchema),
  },
  logs: {
    list: (params?: { sessionId?: string; traceId?: string; spanId?: string; severity?: string; service?: string; page?: number; limit?: number }) => {
      const q = new URLSearchParams()
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.traceId) q.set('traceId', params.traceId)
      if (params?.spanId) q.set('spanId', params.spanId)
      if (params?.severity) q.set('severity', params.severity)
      if (params?.service) q.set('service', params.service)
      if (params?.page) q.set('page', String(params.page))
      if (params?.limit) q.set('limit', String(params.limit))
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
    get: () => get('/api/settings', SettingsResponseSchema),
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
		cardinality: (sessionId?: string) => get(`/api/metrics/cardinality${sessionId ? `?sessionId=${sessionId}` : ''}`, z.array(MetricCardinalityStreamSchema)),
    series: (params: { name: string; service?: string; sessionId?: string; from?: number; to?: number; operation?: string; withTraces?: boolean; dimensionFilters?: Record<string, string> }) => {
      const q = new URLSearchParams({ name: params.name })
      if (params.service) q.set('service', params.service)
      if (params.sessionId) q.set('sessionId', params.sessionId)
      if (params.from) q.set('from', String(params.from))
      if (params.to) q.set('to', String(params.to))
      if (params.operation) q.set('operation', params.operation)
      if (params.withTraces) q.set('with_traces', '1')
			for (const [key, value] of Object.entries(params.dimensionFilters ?? {})) q.set(`attr.${key}`, value)
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
