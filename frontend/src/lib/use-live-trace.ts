import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { onWSEvent, useWSStatus } from './ws'
import { isPageVisible } from './page-visibility'
import type { Span } from './api'

// Refresh authoritative rows after durable ingest, including events without row payloads.
export function useLiveTrace(traceId: string | undefined) {
  const client = useQueryClient()
  const { connected } = useWSStatus()
  const wasConnected = useRef(connected)
  const hasConnected = useRef(connected)
  useEffect(() => {
    if (!traceId) return
    let timer: ReturnType<typeof setTimeout> | undefined
    let traceDirty = false
    const refresh = () => {
      timer = undefined
      if (!isPageVisible()) return
      void client.invalidateQueries(
        {
          predicate: ({ queryKey: [prefix, params] }) =>
            (prefix === 'trace-incoming-links' && params === traceId) ||
            (traceDirty &&
              ((prefix === 'trace' && params === traceId) ||
                (prefix === 'issues' && params === traceId) ||
                prefix === 'lint' ||
                ((prefix === 'logs' || prefix === 'trace-span') &&
                  !!params &&
                  typeof params === 'object' &&
                  'traceId' in params &&
                  params.traceId === traceId))),
        },
        { cancelRefetch: false },
      )
      traceDirty = false
    }
    const unsubscribe = onWSEvent((event) => {
      let id: string | undefined
      switch (event.type) {
        case 'span':
        case 'log':
        case 'issue':
          id = event.payload.traceId
          break
        case 'self_span': {
          if (event.payload.trace_id !== traceId) return
          // Self reads are telemetry too: patch durable rows instead of
          // turning the inspector's own HTTP traces into another refetch.
          const incoming: Span = { ...event.payload, events: [], links: [] }
          client.setQueryData<Span[]>(['trace', traceId], (old) => {
            if (!old) return old
            const current = old.find((span) => span.span_id === incoming.span_id)
            return current
              ? old.map((span) =>
                  span.span_id === incoming.span_id
                    ? { ...incoming, events: span.events, links: span.links }
                    : span,
                )
              : [...old, incoming]
          })
          client.setQueryData<Span>(['trace-span', { traceId, spanId: incoming.span_id }], (old) =>
            old ? { ...incoming, events: old.events, links: old.links } : old,
          )
          return
        }
        case 'self_trace':
        case 'self_log':
          return
        default:
          return
      }
      if (id === traceId) traceDirty = true
      // A span in another trace can link into this trace; coalesce those reads too.
      if ((id === traceId || event.type === 'span') && !timer) timer = setTimeout(refresh, 1000)
    })
    return () => {
      unsubscribe()
      if (timer) clearTimeout(timer)
    }
  }, [client, traceId])
  useEffect(() => {
    if (connected && !wasConnected.current && hasConnected.current && traceId)
      void client.invalidateQueries({ refetchType: 'active' }, { cancelRefetch: false })
    if (connected) hasConnected.current = true
    wasConnected.current = connected
  }, [client, connected, traceId])
}
