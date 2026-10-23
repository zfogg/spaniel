import { useId } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { api, type Log } from '@/lib/api'
import { qk } from '@/lib/query'
import { logKey, logSelectionKey } from '@/lib/log-key'
import ServiceNameBadge from './ServiceNameBadge'
import LogLevelBadge from './LogLevelBadge'
import PaginationControls from './PaginationControls'
import { TimestampWithAgo } from './shared/TimestampWithAgo'

import { TRACE_LOG_PAGE_SIZE } from '@/lib/trace-logs'
const SEVERITIES = ['error', 'fatal', 'warn'] as const

export default function TraceLogPanel({
  traceId,
  sessionId,
  logs,
  total,
  loading,
  error,
  onRetry,
}: {
  traceId: string
  sessionId?: string
  logs: Log[]
  total: number
  loading: boolean
  error: Error | null
  onRetry: () => void
}) {
  const [params, setParams] = useSearchParams()
  const contentId = useId()
  const open = params.get('traceLogs') === 'open'
  const selected = params.get('traceLog')
  const requestedPage = Number(params.get('traceLogsPage') ?? 1)
  const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1
  function updateParams(update: (next: URLSearchParams) => void) {
    // The waterfall also writes its view/span state directly to history.
    const next = new URLSearchParams(window.location.search)
    update(next)
    setParams(next, { replace: true })
  }
  const pageQuery = useQuery({
    queryKey: qk.logs({ traceId, sessionId, page, limit: TRACE_LOG_PAGE_SIZE }),
    queryFn: () => api.logs.list({ traceId, sessionId, page, limit: TRACE_LOG_PAGE_SIZE }),
    enabled: open && page > 1,
  })
  const rows = page === 1 ? logs.slice(0, TRACE_LOG_PAGE_SIZE) : (pageQuery.data?.data ?? [])
  const pending = page === 1 ? loading : pageQuery.isPending
  const pageError = page === 1 ? error : pageQuery.error
  const truncated = total > logs.length
  const hasError = logs.some((log) => log.severity >= 17)
  const hasWarning = logs.some((log) => log.severity >= 13 && log.severity < 17)
  // Check severity beyond the prefetched rows without downloading every page.
  const severityQueries = useQueries({
    queries: SEVERITIES.map((severity) => ({
      queryKey: qk.logs({ traceId, sessionId, severity, page: 1, limit: 1 }),
      queryFn: () => api.logs.list({ traceId, sessionId, severity, page: 1, limit: 1 }),
      enabled: !loading && !error && truncated && (severity === 'warn' ? !hasWarning : !hasError),
    })),
  })
  const present = (index: number) =>
    (severityQueries[index].data?.meta?.total ?? severityQueries[index].data?.data.length ?? 0) > 0
  const errorsPresent = hasError || present(0) || present(1)
  const warningsPresent = hasWarning || present(2)
  const summaryFailed = severityQueries.some((query) => query.isError)
  const countLabel = loading ? 'Loading logs…' : error ? 'Logs unavailable' : `${total} logs`

  return (
    <section
      aria-label="Trace log panel"
      data-testid="trace-log-panel"
      className={`absolute inset-x-0 bottom-0 z-30 flex flex-col border-t border-border bg-background ${open ? 'h-[min(55%,28rem)] min-h-36 shadow-[0_-8px_24px_rgba(0,0,0,0.18)]' : 'h-9'}`}
    >
      <div className="flex h-9 shrink-0 items-center border-b border-border px-3">
        <button
          type="button"
          data-testid="trace-log-toggle"
          aria-label={open ? 'Collapse trace logs' : 'Expand trace logs'}
          aria-expanded={open}
          aria-controls={contentId}
          onClick={() =>
            updateParams((next) => {
              if (open) next.delete('traceLogs')
              else next.set('traceLogs', 'open')
            })
          }
          className="flex h-full flex-1 cursor-pointer items-center gap-2 text-xs"
        >
          {open ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
          <span
            data-testid="trace-log-count"
            data-tone={!loading && !error && total > 0 ? 'green' : 'grey'}
            className={`rounded-full border px-2 py-0.5 font-mono text-[11px] ${!loading && !error && total > 0 ? 'border-emerald-500/30 bg-emerald-500/15 text-emerald-700 dark:text-emerald-300' : 'border-border bg-muted text-muted-foreground'}`}
          >
            {countLabel}
          </span>
          {errorsPresent && (
            <span
              role="img"
              aria-label="Error logs present"
              title="This trace has ERROR or FATAL logs"
              className="rounded border border-red-500/30 bg-red-500/15 px-1.5 py-0.5 font-mono text-[10px] font-bold text-red-700 dark:text-red-300"
            >
              ERROR!
            </span>
          )}
          {warningsPresent && (
            <span
              role="img"
              aria-label="Warning logs present"
              title="This trace has WARNING logs"
              className="rounded border border-yellow-500/30 bg-yellow-500/15 px-1.5 py-0.5 font-mono text-[10px] font-bold text-yellow-800 dark:text-yellow-300"
            >
              WARNING!
            </span>
          )}
        </button>
        {summaryFailed && (
          <button
            type="button"
            className="text-xs text-muted-foreground underline"
            onClick={() =>
              severityQueries.forEach((query) => {
                if (query.isError) void query.refetch()
              })
            }
          >
            Retry log severity checks
          </button>
        )}
      </div>
      {open && (
        <div
          id={contentId}
          role="region"
          aria-label="Trace logs"
          className="flex min-h-0 flex-1 flex-col"
        >
          {pending ? (
            <p role="status" className="p-4 text-xs text-muted-foreground">
              Loading logs…
            </p>
          ) : pageError ? (
            <div role="alert" className="p-4 text-xs">
              <p>Couldn’t load trace logs.</p>
              <button
                type="button"
                className="mt-2 underline"
                onClick={page === 1 ? onRetry : () => void pageQuery.refetch()}
              >
                Retry trace logs
              </button>
            </div>
          ) : rows.length === 0 ? (
            <p className="p-4 text-xs text-muted-foreground">No logs in this trace</p>
          ) : (
            <div className="min-h-0 flex-1 overflow-auto">
              <table className="w-full min-w-[640px] table-fixed border-collapse text-left text-xs">
                <thead className="sticky top-0 z-10 bg-muted text-[10px] uppercase text-muted-foreground">
                  <tr>
                    <th className="w-44 px-3 py-2">Timestamp</th>
                    <th className="w-20 px-2 py-2">Level</th>
                    <th className="w-36 px-2 py-2">Service</th>
                    <th className="px-2 py-2">Message</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((log, index) => {
                    const key = logSelectionKey(log)
                    const expanded = selected === key
                    const toggle = () =>
                      updateParams((next) => {
                        if (expanded) next.delete('traceLog')
                        else next.set('traceLog', key)
                      })
                    return (
                      <tr
                        key={logKey(log)}
                        data-testid="trace-log-row"
                        data-selected={expanded}
                        onClick={toggle}
                        className={`cursor-pointer border-b border-border align-top ${expanded ? 'bg-muted' : 'hover:bg-muted/50'}`}
                      >
                        <td className="px-3 py-2 font-mono text-[10px] text-muted-foreground">
                          <TimestampWithAgo nanoseconds={log.timestamp_ns} stacked />
                        </td>
                        <td className="px-2 py-2">
                          <LogLevelBadge severity={log.severity} />
                        </td>
                        <td className="px-2 py-2">
                          <ServiceNameBadge name={log.service_name} />
                        </td>
                        <td className="min-w-0 px-2 py-2">
                          <button
                            type="button"
                            aria-label={`${expanded ? 'Collapse' : 'Expand'} log message ${(page - 1) * TRACE_LOG_PAGE_SIZE + index + 1}`}
                            aria-expanded={expanded}
                            aria-describedby={`${contentId}-${key}`}
                            onClick={(event) => {
                              event.stopPropagation()
                              toggle()
                            }}
                            className="block w-full min-w-0 cursor-pointer text-left font-mono text-[11px]"
                          >
                            <span
                              id={`${contentId}-${key}`}
                              data-testid="trace-log-message"
                              className={
                                expanded
                                  ? 'block whitespace-pre-wrap [overflow-wrap:anywhere]'
                                  : 'block truncate'
                              }
                            >
                              {log.body || '(empty message)'}
                            </span>
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
          {total > TRACE_LOG_PAGE_SIZE && (
            <div className="shrink-0 border-t border-border px-3 py-2">
              <PaginationControls
                page={page}
                pageSize={TRACE_LOG_PAGE_SIZE}
                total={total}
                itemLabel="trace logs"
                onPageChange={(nextPage) =>
                  updateParams((next) => {
                    if (nextPage === 1) next.delete('traceLogsPage')
                    else next.set('traceLogsPage', String(nextPage))
                    next.delete('traceLog')
                  })
                }
              />
            </div>
          )}
        </div>
      )}
    </section>
  )
}
