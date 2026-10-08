import { openapiClient } from './openapi'
import type { components, operations } from './openapi'

type APIModels = components['schemas']
type CreateAlertInput = NonNullable<
  operations['createAlert']['requestBody']
>['content']['application/json']
type PatchAlertInput = NonNullable<
  operations['patchAlert']['requestBody']
>['content']['application/json']
type Meta = APIModels['Meta']
interface Envelope<T> {
  data: T
  meta?: Meta
}

type GeneratedResponse<T> = {
  data?: Envelope<T>
  error?: APIModels['Error']
  response: Response
}

async function unwrap<T>(call: Promise<GeneratedResponse<T>>): Promise<Envelope<T>> {
  const { data, error, response } = await call
  if (error || !response.ok) {
    throw new Error(error?.error ?? `${response.status} ${response.statusText}`)
  }
  if (!data) throw new Error(`${response.status} ${response.statusText}`)
  return data
}

// ── inferred types (same names callers already import) ────────────────────────

export type TraceRow = APIModels['TraceRow']
export type TraceIssue = APIModels['TraceIssue']
export type SpanEvent = APIModels['SpanEvent']
export type SpanLink = APIModels['SpanLink']
export type Span = APIModels['Span']
export type SpanRow = APIModels['SpanRow']
export type SpanGroup = APIModels['SpanGroup']
export type Log = APIModels['Log']
export type Session = APIModels['Session']
export type ImportResult = APIModels['ImportResult']
export type SearchResult = APIModels['SearchResult']
export type MetricCatalogEntry = APIModels['MetricCatalogEntry']
export type MetricSeriesExemplar = APIModels['MetricSeriesExemplar']
export type MetricSeriesPoint = APIModels['MetricSeriesPoint']
export type TraceOverlay = APIModels['TraceOverlay']
export type MetricSeries = APIModels['MetricSeries']
export type MetricCardinalityStream = APIModels['MetricCardinalityStream']
export type CoverageRoute = APIModels['CoverageRoute']
export type ServiceCoverage = APIModels['ServiceCoverage']
export type CoverageReport = APIModels['CoverageReport']
export type CoverageSpec = APIModels['CoverageSpec']
export type CoverageSpecInput = APIModels['CoverageSpecInput']
export type SettingsRuntime = APIModels['SettingsRuntime']
export type UpdateCheckResult = APIModels['UpdateCheckResult']
export type Settings = APIModels['Settings']
export type SourceStats = APIModels['SourceStats']
export type ForwarderStatus = APIModels['ForwarderStatus']
export type LintWarning = APIModels['LintWarning']
export type Stats = APIModels['Stats']
export type ServiceMapOpStat = APIModels['ServiceMapOpStat']
export type ServiceMapNode = APIModels['ServiceMapNode']
export type ServiceMapEdge = APIModels['ServiceMapEdge']
export type ServiceMapData = APIModels['ServiceMapData']
export type TableStat = APIModels['TableStat']
export type SessionSize = APIModels['SessionSize']
export type StorageBreakdown = APIModels['StorageBreakdown']
export type CompactResult = APIModels['CompactResult']
export type PruneResult = APIModels['PruneResult']
export type Dashboard = APIModels['Dashboard']
export type DashboardPanel = APIModels['DashboardPanel']
export type DashboardVariable = APIModels['DashboardVariable']
export type QueryPreview = APIModels['QueryPreview']
export type QueryCatalogEntry = APIModels['QueryCatalogEntry']
export type AlertRule = APIModels['AlertRule']
export type AlertEvent = APIModels['AlertEvent']
export type AlertSilence = APIModels['AlertSilence']
export type AlertEventsEnvelope = Envelope<AlertEvent[]> & { silences: AlertSilence[] }
export type NotificationRecord = APIModels['NotificationRecord']
export type AlertList = APIModels['AlertList']

// Request payload — not a response, so no runtime validation needed.
export interface SettingsUpdate {
  port?: number
  db_path?: string
  alerts_dir?: string
  retention_days?: number
  advance_session_on_start?: boolean
  max_sessions?: number
  max_db_size_mb?: number
  auto_prune?: boolean
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
  alerts_browser_enabled?: boolean
  alerts_pushover_enabled?: boolean
  alerts_browser_template?: string
  alerts_pushover_template?: string
}

const isSpanRow = (row: SpanRow | SpanGroup): row is SpanRow => 'span_id' in row
const isSpanGroup = (row: SpanRow | SpanGroup): row is SpanGroup => 'count' in row

export const api = {
  databaseSchema: {
    get: () => unwrap(openapiClient.GET('/api/database-schema')),
  },
  dashboards: {
    list: () => unwrap(openapiClient.GET('/api/dashboards')),
    get: (id: string) =>
      unwrap(openapiClient.GET('/api/dashboards/{id}', { params: { path: { id } } })),
    create: (body: {
      name: string
      description?: string
      panels?: Array<Pick<DashboardPanel, 'title' | 'display_type' | 'query_sql'>>
    }) => unwrap(openapiClient.POST('/api/dashboards', { body })),
    update: (id: string, body: { name: string; description?: string }) =>
      unwrap(openapiClient.PATCH('/api/dashboards/{id}', { params: { path: { id } }, body })),
    remove: (id: string) =>
      unwrap(openapiClient.DELETE('/api/dashboards/{id}', { params: { path: { id } } })),
    config: async (id: string) => {
      const response = await fetch(`/api/dashboards/${id}/config`)
      if (!response.ok) throw new Error(await response.text())
      return response.text()
    },
    importConfig: async (yaml: string) => {
      const response = await fetch('/api/dashboards/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/yaml' },
        body: yaml,
      })
      if (!response.ok) throw new Error(await response.text())
      return response.json() as Promise<Envelope<Dashboard>>
    },
    preview: (
      id: string,
      body: {
        query_sql: string
        name?: string
        display_type?: string
        variables?: Record<string, string>
      },
      signal?: AbortSignal,
    ) =>
      unwrap(
        openapiClient.POST('/api/dashboards/{id}/query-preview', {
          params: { path: { id } },
          body,
          signal,
        }),
      ),
    catalog: (signal?: string, search?: string, abortSignal?: AbortSignal) => {
      return unwrap(
        openapiClient.GET('/api/query-catalog', {
          params: { query: { signal, q: search } },
          signal: abortSignal,
        }),
      )
    },
    panel: (
      id: string,
      body: {
        title: string
        display_type: string
        query_sql: string
        settings_json?: string
        layout_json?: string
        position: number
      },
    ) =>
      unwrap(
        openapiClient.POST('/api/dashboards/{id}/panels', {
          params: { path: { id } },
          body,
        }),
      ),
    updatePanel: (
      id: string,
      panelId: string,
      body: {
        title: string
        display_type: string
        query_sql: string
        settings_json?: string
        layout_json?: string
        position: number
      },
    ) =>
      unwrap(
        openapiClient.PATCH('/api/dashboards/{id}/panels/{panelId}', {
          params: { path: { id, panelId } },
          body,
        }),
      ),
    removePanel: (id: string, panelId: string) =>
      unwrap(
        openapiClient.DELETE('/api/dashboards/{id}/panels/{panelId}', {
          params: { path: { id, panelId } },
        }),
      ),
    movePanel: (id: string, panelId: string, direction: -1 | 1) =>
      unwrap(
        openapiClient.POST('/api/dashboards/{id}/panels/{panelId}/move', {
          params: { path: { id, panelId } },
          body: { direction },
        }),
      ),
    variable: (
      id: string,
      body: {
        name: string
        kind: string
        source: string
        options_json?: string
        default_value?: string
      },
    ) =>
      unwrap(
        openapiClient.POST('/api/dashboards/{id}/variables', {
          params: { path: { id } },
          body,
        }),
      ),
    deleteVariable: (id: string, name: string) =>
      unwrap(
        openapiClient.DELETE('/api/dashboards/{id}/variables/{name}', {
          params: { path: { id, name } },
        }),
      ),
    reorder: (ids: string[]) =>
      unwrap(openapiClient.POST('/api/dashboards/reorder', { body: { ids } })),
  },
  alerts: {
    list: ({
      page = 1,
      limit = 15,
      state,
      search,
    }: { page?: number; limit?: number; state?: string; search?: string } = {}) => {
      return unwrap(
        openapiClient.GET('/api/alerts', { params: { query: { page, limit, state, search } } }),
      )
    },
    get: (id: string) =>
      unwrap(openapiClient.GET('/api/alerts/{id}', { params: { path: { id } } })),
    create: (body: CreateAlertInput) => unwrap(openapiClient.POST('/api/alerts', { body })),
    update: (id: string, body: PatchAlertInput) =>
      unwrap(openapiClient.PATCH('/api/alerts/{id}', { params: { path: { id } }, body })),
    duplicate: (id: string) =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/duplicate', { params: { path: { id } }, body: {} }),
      ),
    testNotification: (id: string, destination: 'browser' | 'pushover') =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/test-notification', {
          params: { path: { id } },
          body: { destination },
        }),
      ),
    remove: (id: string) =>
      unwrap(openapiClient.DELETE('/api/alerts/{id}', { params: { path: { id } } })),
    acknowledge: (id: string) =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/acknowledge', { params: { path: { id } }, body: {} }),
      ),
    acknowledgeInstance: (id: string, groupKey: string, note = '') =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/instances/acknowledge', {
          params: { path: { id } },
          body: { group_key: groupKey, note },
        }),
      ),
    unacknowledgeInstance: (id: string, groupKey: string) =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/instances/unacknowledge', {
          params: { path: { id } },
          body: { group_key: groupKey },
        }),
      ),
    preview: (id: string) =>
      unwrap(
        openapiClient.POST('/api/alerts/{id}/preview', { params: { path: { id } }, body: {} }),
      ),
    previewDraft: (
      body: NonNullable<
        operations['previewAlertDraft']['requestBody']
      >['content']['application/json'],
    ) => unwrap(openapiClient.POST('/api/alerts/preview', { body })),
    config: async (id: string) => {
      const r = await fetch(`/api/alerts/${id}/config`)
      if (!r.ok) throw new Error(await r.text())
      return r.text()
    },
    importConfig: async (yaml: string) => {
      const r = await fetch('/api/alerts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/yaml' },
        body: yaml,
      })
      if (!r.ok) throw new Error(await r.text())
      return r.json()
    },
    reload: () => unwrap(openapiClient.POST('/api/alerts/reload', { body: {} })),
    events: (
      id: string,
      { groupKey, page = 1, limit = 15 }: { groupKey?: string; page?: number; limit?: number } = {},
    ) => {
      return unwrap(
        openapiClient.GET('/api/alerts/{id}/events', {
          params: { path: { id }, query: { group_key: groupKey, page, limit } },
        }),
      ).then((response) => response as AlertEventsEnvelope)
    },
    history: (
      filters: {
        page?: number
        limit?: number
        rule_id?: string
        state?: string
        kind?: string
        severity?: string
        group_key?: string
        search?: string
        from?: number
        to?: number
      } = {},
    ) => {
      return unwrap(openapiClient.GET('/api/alerts/history', { params: { query: filters } }))
    },
    silence: (
      id: string,
      body: { ends_at: number; comment: string; group_key?: string; starts_at?: number },
    ) =>
      unwrap(openapiClient.POST('/api/alerts/{id}/silences', { params: { path: { id } }, body })),
    updateSilence: (
      id: string,
      silenceID: string,
      body: { ends_at: number; comment: string; group_key?: string; starts_at: number },
    ) =>
      unwrap(
        openapiClient.PATCH('/api/alerts/{id}/silences/{silenceID}', {
          params: { path: { id, silenceID } },
          body,
        }),
      ),
    removeSilence: (id: string, silenceID: string) =>
      unwrap(
        openapiClient.DELETE('/api/alerts/{id}/silences/{silenceID}', {
          params: { path: { id, silenceID } },
        }),
      ),
  },
  notifications: {
    list: ({
      page = 1,
      limit = 30,
      source,
    }: { page?: number; limit?: number; source?: string } = {}) =>
      unwrap(
        openapiClient.GET('/api/notifications', { params: { query: { page, limit, source } } }),
      ),
    read: (id: string) =>
      unwrap(openapiClient.POST('/api/notifications/{id}/read', { params: { path: { id } } })),
    acknowledge: (id: string) =>
      unwrap(
        openapiClient.POST('/api/notifications/{id}/acknowledge', {
          params: { path: { id } },
        }),
      ),
  },
  traces: {
    list: (
      params: { sessionId?: string; service?: string; page?: number; limit?: number } = {},
    ) => {
      return unwrap(openapiClient.GET('/api/traces', { params: { query: params } }))
    },
    get: (traceId: string) =>
      unwrap(openapiClient.GET('/api/traces/{traceId}', { params: { path: { traceId } } })),
    exportUrl: (traceId: string) => `/api/traces/${traceId}/export`,
  },
  spans: {
    list: (params?: {
      sort?: string
      sessionId?: string
      limit?: number
      page?: number
      service?: string
      name?: string
      kind?: number
    }) =>
      unwrap(openapiClient.GET('/api/spans', { params: { query: params } })).then((response) => ({
        ...response,
        data: response.data.filter(isSpanRow),
      })),
    groups: (params?: { sessionId?: string; limit?: number; page?: number }) => {
      return unwrap(
        openapiClient.GET('/api/spans', { params: { query: { ...params, view: 'grouped' } } }),
      ).then((response) => ({ ...response, data: response.data.filter(isSpanGroup) }))
    },
    get: (spanId: string) =>
      unwrap(openapiClient.GET('/api/spans/{spanId}', { params: { path: { spanId } } })),
  },
  logs: {
    list: (params?: {
      sessionId?: string
      traceId?: string
      spanId?: string
      severity?: string
      service?: string
      page?: number
      limit?: number
    }) => {
      return unwrap(openapiClient.GET('/api/logs', { params: { query: params } }))
    },
  },
  services: {
    list: () => unwrap(openapiClient.GET('/api/services')),
  },
  sessions: {
    list: () => unwrap(openapiClient.GET('/api/sessions')),
    get: (id: string) =>
      unwrap(
        openapiClient.GET('/api/sessions/{sessionId}', { params: { path: { sessionId: id } } }),
      ),
    getActive: () => unwrap(openapiClient.GET('/api/sessions/active')),
    create: (label?: string) => unwrap(openapiClient.POST('/api/sessions', { body: { label } })),
    activate: (id: string) =>
      unwrap(
        openapiClient.POST('/api/sessions/{sessionId}/activate', {
          params: { path: { sessionId: id } },
          body: {},
        }),
      ),
    baseline: (id: string, isBaseline: boolean) =>
      unwrap(
        openapiClient.POST('/api/sessions/{sessionId}/baseline', {
          params: { path: { sessionId: id } },
          body: { is_baseline: isBaseline },
        }),
      ),
    patch: (id: string, body: { label?: string; note?: string }) =>
      unwrap(
        openapiClient.PATCH('/api/sessions/{sessionId}', {
          params: { path: { sessionId: id } },
          body,
        }),
      ),
    delete: (id: string) =>
      unwrap(
        openapiClient.DELETE('/api/sessions/{sessionId}', { params: { path: { sessionId: id } } }),
      ),
    import: (label: string, format: string, data: string) =>
      unwrap(
        openapiClient.POST('/api/sessions/import', {
          params: { query: { label, format } },
          body: data,
        }),
      ),
  },
  lint: {
    list: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/lint', { params: { query: { sessionId } } })),
  },
  stats: {
    get: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/stats', { params: { query: { sessionId } } })),
  },
  serviceMap: {
    get: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/service-map', { params: { query: { sessionId } } })),
  },
  issues: {
    get: (traceId: string) =>
      unwrap(openapiClient.GET('/api/issues', { params: { query: { traceId } } })),
    list: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/issues', { params: { query: { sessionId } } })),
  },
  health: {
    get: () => unwrap(openapiClient.GET('/api/health')),
    // Presents the bearer token to /api/health so the server sets the auth cookie.
    seed: (token: string): Promise<void> =>
      fetch('/api/health', { headers: { Authorization: `Bearer ${token}` } }).then(() => undefined),
  },
  sources: {
    list: () => unwrap(openapiClient.GET('/api/sources')),
  },
  forwarders: {
    list: () => unwrap(openapiClient.GET('/api/forwarders')),
  },
  settings: {
    get: () => unwrap(openapiClient.GET('/api/settings')),
    update: (patchBody: SettingsUpdate) =>
      unwrap(openapiClient.PUT('/api/settings', { body: patchBody })),
    dropAllData: () => unwrap(openapiClient.DELETE('/api/settings/data')),
    compact: () => unwrap(openapiClient.POST('/api/settings/compact', { body: {} })),
    prune: () => unwrap(openapiClient.POST('/api/settings/prune', { body: {} })),
    checkUpdates: () => unwrap(openapiClient.POST('/api/settings/check-updates', { body: {} })),
  },
  storage: {
    get: () => unwrap(openapiClient.GET('/api/storage')),
  },
  coverage: {
    get: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/coverage', { params: { query: { sessionId } } })),
    specs: () =>
      unwrap(openapiClient.GET('/api/coverage/specs')) as Promise<Envelope<CoverageSpec[]>>,
    createSpec: (body: CoverageSpecInput) =>
      unwrap(openapiClient.POST('/api/coverage/specs', { body })) as Promise<
        Envelope<CoverageSpec>
      >,
    replaceSpec: (id: string, body: CoverageSpecInput) =>
      unwrap(
        openapiClient.PUT('/api/coverage/specs/{id}', { params: { path: { id } }, body }),
      ) as Promise<Envelope<CoverageSpec>>,
    deleteSpec: (id: string) =>
      unwrap(
        openapiClient.DELETE('/api/coverage/specs/{id}', { params: { path: { id } } }),
      ) as Promise<Envelope<{ ok: boolean }>>,
    getSpec: (id: string) =>
      unwrap(
        openapiClient.GET('/api/coverage/specs/{id}', { params: { path: { id } } }),
      ) as Promise<Envelope<CoverageSpec>>,
  },
  metrics: {
    list: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/metrics', { params: { query: { sessionId } } })),
    cardinality: (sessionId?: string) =>
      unwrap(openapiClient.GET('/api/metrics/cardinality', { params: { query: { sessionId } } })),
    series: (params: {
      name: string
      service?: string
      sessionId?: string
      from?: number
      to?: number
      operation?: string
      withTraces?: boolean
      dimensionFilters?: Record<string, string>
    }) =>
      unwrap(
        openapiClient.GET('/api/metrics/series', {
          params: {
            query: {
              name: params.name,
              service: params.service,
              sessionId: params.sessionId,
              from: params.from,
              to: params.to,
              operation: params.operation,
              with_traces: params.withTraces,
              attributes: params.dimensionFilters,
            },
          },
        }),
      ),
  },
  search: {
    query: (q: string, sessionId?: string) =>
      unwrap(openapiClient.GET('/api/search', { params: { query: { q, limit: 20, sessionId } } })),
  },
}
