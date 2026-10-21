import { queryOptions } from '@tanstack/react-query'
import { api, type Log } from './api'
import { qk } from './query'

export const logTraceQuery = (traceId: string) =>
  queryOptions({
    queryKey: qk.trace(traceId),
    queryFn: () => api.traces.get(traceId).then((response) => response.data ?? []),
  })

export function logTraceUrl(log: Pick<Log, 'trace_id' | 'span_id'>) {
  const span =
    log.span_id && !/^0+$/.test(log.span_id) ? `?spanId=${encodeURIComponent(log.span_id)}` : ''
  return `/traces/${encodeURIComponent(log.trace_id)}${span}`
}
