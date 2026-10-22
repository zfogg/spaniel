import { useEffect } from 'react'
import { QueryClient, useQueryClient } from '@tanstack/react-query'
import type {
  ForwarderStatus,
  Log,
  MetricCatalogEntry,
  MetricSeries,
  MetricSeriesPoint,
  SourceStats,
  SpanRow,
  Stats,
  TraceRow,
} from './api'
import { onWSEvent } from './ws'
import { isPageVisible } from './page-visibility'

// Single shared client. This is a local dev tool talking to localhost, so we
// keep data briefly fresh and avoid focus/aggressive-retry refetch noise; live
// freshness comes from WebSocket-driven invalidation (useLiveInvalidation).
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})

// Query-key registry. Keep keys here so pages and the live invalidator agree on
// the same prefixes. The first element of every key is the invalidation prefix
// used by useLiveInvalidation below.
export const qk = {
  traces: (p?: Record<string, unknown>) => ['traces', p ?? null] as const,
  trace: (id: string) => ['trace', id] as const,
  spans: (p?: Record<string, unknown>) => ['spans', p ?? null] as const,
  span: (id: string) => ['span', id] as const,
  logs: (p?: Record<string, unknown>) => ['logs', p ?? null] as const,
  metrics: (sessionId?: string) => ['metrics', sessionId ?? null] as const,
  metricSeries: (p: Record<string, unknown>) => ['metric-series', p] as const,
  serviceMap: (sessionId?: string) => ['service-map', sessionId ?? null] as const,
  coverage: (sessionId?: string) => ['coverage', sessionId ?? null] as const,
  sessions: () => ['sessions'] as const,
  session: (id: string) => ['session', id] as const,
  activeSession: () => ['active-session'] as const,
  issues: (key?: string) => ['issues', key ?? null] as const,
  lint: (sessionId?: string) => ['lint', sessionId ?? null] as const,
  stats: (sessionId?: string) => ['stats', sessionId ?? null] as const,
  services: (signal?: string, sessionId?: string) =>
    ['services', signal ?? null, sessionId ?? null] as const,
  forwarders: () => ['forwarders'] as const,
  sources: () => ['sources'] as const,
  storage: () => ['storage'] as const,
  settings: () => ['settings'] as const,
  dashboards: () => ['dashboards'] as const,
  alerts: (p?: Record<string, unknown>) => ['alerts', p ?? null] as const,
  notifications: () => ['notifications'] as const,
}

// Maps a live WebSocket event type to the query-key prefixes it should
// invalidate. Throughput is intentionally absent — BottomBar consumes it from
// the live stream directly, it is not query state.
const INVALIDATIONS: Record<string, string[]> = {
  // Dashboard panels execute read-only queries over these same live streams.
  // Invalidate their active observers from the event that made their results
  // stale, rather than polling the preview endpoint.
  span: [
    'services',
    'trace',
    'span',
    'service-map',
    'lint',
    'coverage',
    'sessions',
    'dashboard-panel',
  ],
  log: ['services', 'dashboard-panel'],
  // The Metrics view refreshes its selected stream from matching metric
  // events. Do not also refetch it for every unrelated metric in the store.
  metric: ['services', 'dashboard-panel'],
  issue: ['issues', 'trace'],
  alert: ['alerts', 'notifications'],
}

type TraceListData = { data: TraceRow[]; meta?: { page?: number; total?: number } }
type ListData<T> = { data: T[]; meta?: { page?: number; total?: number } }

function matchesLogSeverity(value: number, filter: unknown) {
  if (filter === undefined || filter === null) return true
  const minimum: Record<string, number> = {
    trace: 1,
    debug: 5,
    info: 9,
    warn: 13,
    error: 17,
    fatal: 21,
  }
  return typeof filter === 'string' && value >= (minimum[filter] ?? Number.POSITIVE_INFINITY)
}

function parseNumberArray(value?: string): number[] | undefined {
  if (!value) return undefined
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? parsed.map(Number) : undefined
  } catch {
    return undefined
  }
}

function metricPointFromEvent(
  row: NonNullable<Extract<import('./ws').WsEvent, { type: 'metric' }>['payload']['row']>,
  operation: unknown,
  previous?: MetricSeriesPoint,
): MetricSeriesPoint {
  const bounds = parseNumberArray(row.explicit_bounds)
  const buckets = parseNumberArray(row.bucket_counts)
  let value = row.value ?? row.histogram_sum ?? row.summary_sum ?? 0
  if (operation === 'count') value = row.histogram_count ?? row.summary_count ?? 0
  if (operation === 'sum') value = row.histogram_sum ?? row.summary_sum ?? value
  if (operation === 'min') value = row.histogram_min ?? value
  if (operation === 'max') value = row.histogram_max ?? value
  if ((operation === 'delta' || operation === 'rate') && previous) {
    const delta = value - previous.value
    value =
      operation === 'rate'
        ? delta / Math.max(1e-9, (row.timestamp_ns - previous.timestamp_ns) / 1e9)
        : delta
  }
  if (typeof operation === 'string' && /^p(50|95|99)$/.test(operation) && bounds && buckets) {
    const percentile = Number(operation.slice(1)) / 100
    const total = buckets.reduce((sum, count) => sum + count, 0)
    const target = total * percentile
    let seen = 0
    for (let i = 0; i < buckets.length; i++) {
      seen += buckets[i]
      if (seen >= target) {
        value = bounds[Math.min(i, bounds.length - 1)] ?? row.histogram_max ?? value
        break
      }
    }
  }
  return {
    timestamp_ns: row.timestamp_ns,
    start_timestamp_ns: row.start_timestamp_ns,
    flags: row.flags,
    value,
    count: row.histogram_count ?? row.summary_count,
    sum: row.histogram_sum ?? row.summary_sum,
    min: row.histogram_min,
    max: row.histogram_max,
    bounds,
    buckets,
    scope_name: row.scope_name,
    scope_version: row.scope_version,
    scope_schema_url: row.scope_schema_url,
  }
}

export function patchSelfTrace(
  old: TraceListData | undefined,
  trace: TraceRow,
): TraceListData | undefined {
  if (!old) return old
  if (old.data.some((item) => item.trace_id === trace.trace_id)) return old
  return {
    ...old,
    data: [trace, ...old.data].slice(0, Math.max(old.data.length, 100)),
    meta: old.meta ? { ...old.meta, total: (old.meta.total ?? 0) + 1 } : old.meta,
  }
}

function patchFirstPage<T>(
  old: ListData<T> | undefined,
  item: T,
  exists: (current: T) => boolean,
): ListData<T> | undefined {
  if (!old || old.data.some(exists)) return old
  return {
    ...old,
    data: [item, ...old.data].slice(0, Math.max(old.data.length, 100)),
    meta: old.meta ? { ...old.meta, total: (old.meta.total ?? 0) + 1 } : old.meta,
  }
}

// Opens a single WebSocket and turns live events into throttled query
// invalidations. Forwarder status is the exception: its complete per-upstream
// snapshot arrives in the event, so it updates the cache directly without an
// HTTP refetch. Mount exactly once near the app root. Invalidations are
// coalesced (~1/sec per prefix) so a burst of spans does not trigger a refetch
// per message. invalidateQueries only refetches active observers, so marking a
// broad set of prefixes is cheap when those views aren't mounted.
export function useLiveInvalidation() {
  const qc = useQueryClient()
  useEffect(() => {
    const dirty = new Set<string>()
    const liveTraceSpanCounts = new Map<string, number>()
    let timer: ReturnType<typeof setTimeout> | null = null
    let visible = isPageVisible()

    const flush = () => {
      timer = null
      if (!isPageVisible()) return
      for (const prefix of dirty)
        qc.invalidateQueries({ queryKey: [prefix] }, { cancelRefetch: false })
      dirty.clear()
    }

    const onVisibilityChange = () => {
      const nextVisible = isPageVisible()
      if (nextVisible === visible) return
      visible = nextVisible
      if (timer) clearTimeout(timer)
      timer = null
      if (!isPageVisible()) {
        // Cancel the expensive reads already in flight. Their fetch functions
        // consume the signal so DuckDB also sees the canceled request context.
        void qc.cancelQueries({ queryKey: ['dashboard-panel'] })
        void qc.cancelQueries({ queryKey: ['dashboard-annotations'] })
        void qc.cancelQueries({ queryKey: ['metric-series'] })
        return
      }
      dirty.clear()
      // Include disabled/inactive entries when marking stale, but fetch only
      // mounted, enabled views. A visibility-enabled observer may re-render
      // just after this event; it must see stale data and perform one refresh.
      void qc.invalidateQueries({ refetchType: 'active' }, { cancelRefetch: false })
    }
    document.addEventListener('visibilitychange', onVisibilityChange)

    const unsub = onWSEvent((ev) => {
      if (ev.type === 'span' && ev.payload.row) {
        const row = ev.payload.row
        const liveSpanCount = (liveTraceSpanCounts.get(row.trace_id) ?? 0) + 1
        liveTraceSpanCounts.set(row.trace_id, liveSpanCount)
        qc.setQueriesData<ListData<SpanRow>>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'spans' || !params || typeof params !== 'object') return false
              const { page, sort, view, services, sessionId } = params as Record<string, unknown>
              return (
                (page === 1 || page === undefined) &&
                sort === 'time' &&
                view === undefined &&
                (sessionId === undefined || sessionId === null || sessionId === row.session_id) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(row.service_name))
              )
            },
          },
          (old) => patchFirstPage(old, row, (span) => span.span_id === row.span_id),
        )
        qc.setQueriesData<TraceListData>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'traces' || !params || typeof params !== 'object') return false
              if (!ev.payload.trace) return true
              const { page, services, sessionId } = params as Record<string, unknown>
              return (
                (page === 1 || page === undefined) &&
                (sessionId === undefined ||
                  sessionId === null ||
                  sessionId === ev.payload.trace.session_id) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(ev.payload.trace.service_name))
              )
            },
          },
          (old) => {
            if (!old) return old
            if (ev.payload.trace) {
              const incoming = { ...ev.payload.trace, span_count: liveSpanCount }
              const existing = old.data.findIndex((trace) => trace.trace_id === incoming.trace_id)
              if (existing === -1) return patchSelfTrace(old, incoming)
              return {
                ...old,
                data: old.data.map((trace, index) =>
                  index === existing
                    ? {
                        ...trace,
                        ...incoming,
                        span_count: Math.max(trace.span_count, liveSpanCount),
                      }
                    : trace,
                ),
              }
            }
            return {
              ...old,
              data: old.data.map((trace) =>
                trace.trace_id === row.trace_id
                  ? { ...trace, span_count: Math.max(trace.span_count + 1, liveSpanCount) }
                  : trace,
              ),
            }
          },
        )
      }
      if (ev.type === 'log' && ev.payload.row) {
        const row = ev.payload.row
        qc.setQueriesData<ListData<Log>>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'logs' || !params || typeof params !== 'object') return false
              const { page, severity, traceId, services, sessionId, fromNs } = params as Record<
                string,
                unknown
              >
              return (
                (page === 1 || page === undefined) &&
                matchesLogSeverity(row.severity, severity) &&
                (traceId === undefined || traceId === null || traceId === row.trace_id) &&
                (sessionId === undefined || sessionId === null || sessionId === row.session_id) &&
                (fromNs === undefined || fromNs === null || row.timestamp_ns >= Number(fromNs)) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(row.service_name))
              )
            },
          },
          (old) =>
            patchFirstPage(
              old,
              row,
              (log) =>
                log.timestamp_ns === row.timestamp_ns &&
                log.span_id === row.span_id &&
                log.body === row.body,
            ),
        )
      }
      if (ev.type === 'metric' && ev.payload.row) {
        const row = ev.payload.row
        qc.setQueriesData<MetricCatalogEntry[]>(
          { predicate: (query) => query.queryKey[0] === 'metrics' },
          (old) => {
            if (!old) return old
            const index = old.findIndex(
              (metric) => metric.name === row.name && metric.service_name === row.service_name,
            )
            if (index === -1)
              return [
                {
                  name: row.name,
                  description: row.description,
                  unit: row.unit,
                  type: row.type,
                  service_name: row.service_name,
                  sample_count: 1,
                  last_timestamp_ns: row.timestamp_ns,
                  aggregation_temporality: row.aggregation_temporality,
                  is_monotonic: row.is_monotonic,
                },
                ...old,
              ]
            return old.map((metric, i) =>
              i === index
                ? {
                    ...metric,
                    sample_count: metric.sample_count + 1,
                    last_timestamp_ns: Math.max(metric.last_timestamp_ns, row.timestamp_ns),
                  }
                : metric,
            )
          },
        )
        for (const [key, old] of qc.getQueriesData<MetricSeries | null>({
          queryKey: ['metric-series'],
        })) {
          const params = key[1]
          if (!params || typeof params !== 'object' || !old) continue
          const { name, service, sessionId, operation } = params as Record<string, unknown>
          if (
            name !== row.name ||
            service !== row.service_name ||
            (sessionId !== undefined && sessionId !== null && sessionId !== row.session_id)
          )
            continue
          const point = metricPointFromEvent(row, operation, old.points[old.points.length - 1])
          if (old.points.some((current) => current.timestamp_ns === point.timestamp_ns)) continue
          qc.setQueryData<MetricSeries>(key, { ...old, points: [...old.points, point] })
        }
      }
      if (ev.type === 'self_trace') {
        // This is a cache patch, deliberately not an invalidation. Refetching
        // /api/traces would itself be captured as self telemetry and loop.
        qc.setQueriesData<TraceListData>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'traces' || !params || typeof params !== 'object') return false
              const { page, service, services, sessionId } = params as Record<string, unknown>
              return (
                (page === 1 || page === undefined) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(ev.payload.service_name)) &&
                (service === 'all' ||
                  service === undefined ||
                  service === ev.payload.service_name) &&
                (sessionId === undefined ||
                  sessionId === null ||
                  sessionId === ev.payload.session_id)
              )
            },
          },
          (old) => patchSelfTrace(old, ev.payload),
        )
        return
      }
      if (ev.type === 'self_span') {
        qc.setQueriesData<ListData<SpanRow>>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'spans' || !params || typeof params !== 'object') return false
              const { page, sort, drilldown, view, services, sessionId } = params as Record<
                string,
                unknown
              >
              return (
                (page === 1 || page === undefined) &&
                sort === 'time' &&
                (sessionId === undefined ||
                  sessionId === null ||
                  sessionId === ev.payload.session_id) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(ev.payload.service_name)) &&
                drilldown === null &&
                view === undefined
              )
            },
          },
          (old) => patchFirstPage(old, ev.payload, (span) => span.span_id === ev.payload.span_id),
        )
        return
      }
      if (ev.type === 'self_log') {
        qc.setQueriesData<ListData<Log>>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'logs' || !params || typeof params !== 'object') return false
              const { page, severity, traceId, service, services, sessionId } = params as Record<
                string,
                unknown
              >
              return (
                (page === 1 || page === undefined) &&
                severity === undefined &&
                traceId === undefined &&
                (sessionId === undefined ||
                  sessionId === null ||
                  sessionId === ev.payload.session_id) &&
                (!Array.isArray(services) ||
                  services.length === 0 ||
                  services.includes(ev.payload.service_name)) &&
                (service === 'all' || service === undefined || service === ev.payload.service_name)
              )
            },
          },
          (old) =>
            patchFirstPage(
              old,
              ev.payload,
              (log) =>
                log.timestamp_ns === ev.payload.timestamp_ns &&
                log.span_id === ev.payload.span_id &&
                log.body === ev.payload.body,
            ),
        )
        return
      }
      // Footer resources are bootstrapped over HTTP once, then replaced by a
      // server-produced snapshot after durable ingest. Do not invalidate these
      // keys: that would turn a push event back into a client-side poll.
      if (ev.type === 'live_state') {
        qc.setQueryData<Stats>(qk.stats(), ev.payload.stats)
        qc.setQueryData<SourceStats[]>(qk.sources(), ev.payload.sources)
        return
      }
      if (ev.type === 'active_session') {
        qc.setQueryData(qk.activeSession(), ev.payload)
        return
      }
      if (ev.type === 'forwarder') {
        const status: ForwarderStatus = {
          url: ev.payload.url,
          sent: ev.payload.sent,
          errors: ev.payload.errors,
          last_error: ev.payload.lastError,
          pending_bytes: ev.payload.pendingBytes,
          dropped_spool: ev.payload.droppedSpool,
        }
        qc.setQueryData<ForwarderStatus[]>(qk.forwarders(), (previous = []) => {
          const index = previous.findIndex((item) => item.url === status.url)
          if (index === -1) return [...previous, status]
          return previous.map((item, i) => (i === index ? status : item))
        })
        return
      }
      // Self-telemetry catalog frames update the Metrics sidebar locally. They
      // must not refetch telemetry queries, which would form a feedback loop.
      if (ev.type === 'metric' && ev.payload.catalogOnly) return
      const prefixes = INVALIDATIONS[ev.type]
      if (!prefixes) return
      for (const p of prefixes) dirty.add(p)
      if (isPageVisible() && !timer) timer = setTimeout(flush, 1_000)
    })

    return () => {
      unsub()
      document.removeEventListener('visibilitychange', onVisibilityChange)
      if (timer) clearTimeout(timer)
    }
  }, [qc])
}
