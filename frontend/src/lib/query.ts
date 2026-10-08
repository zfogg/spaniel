import { useEffect } from 'react'
import { QueryClient, useQueryClient } from '@tanstack/react-query'
import type { ForwarderStatus, SourceStats, Stats, TraceRow } from './api'
import { onWSEvent } from './ws'

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
  services: () => ['services'] as const,
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
    'traces',
    'trace',
    'spans',
    'span',
    'service-map',
    'lint',
    'coverage',
    'sessions',
    'dashboard-panel',
  ],
  log: ['logs', 'dashboard-panel'],
  metric: ['metrics', 'metric-series', 'dashboard-panel'],
  issue: ['issues', 'traces', 'trace'],
  alert: ['alerts', 'notifications'],
}

type TraceListData = { data: TraceRow[]; meta?: { page?: number; total?: number } }

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
    let timer: ReturnType<typeof setTimeout> | null = null

    const flush = () => {
      timer = null
      for (const prefix of dirty) qc.invalidateQueries({ queryKey: [prefix] })
      dirty.clear()
    }

    const unsub = onWSEvent((ev) => {
      if (ev.type === 'self_trace') {
        // This is a cache patch, deliberately not an invalidation. Refetching
        // /api/traces would itself be captured as self telemetry and loop.
        qc.setQueriesData<TraceListData>(
          {
            predicate: (query) => {
              const [prefix, params] = query.queryKey
              if (prefix !== 'traces' || !params || typeof params !== 'object') return false
              const { page, service, sessionId } = params as Record<string, unknown>
              return (
                (page === 1 || page === undefined) &&
                (service === 'all' || service === undefined) &&
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
      if (!timer) timer = setTimeout(flush, 1_000)
    })

    return () => {
      unsub()
      if (timer) clearTimeout(timer)
    }
  }, [qc])
}
