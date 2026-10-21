import LogLevelBadge from '@/components/LogLevelBadge'
import { formatValue, columnValue } from './format'
import { Link } from 'react-router-dom'
import type { DashboardPanelRendererProps } from './types'

function logTime(row: Record<string, unknown>): Date | undefined {
  const nanos = row.timestamp_ns ?? row.time_ns
  const value = nanos ?? row.timestamp ?? row.time
  if (value == null || value === '') return undefined
  const date =
    nanos != null
      ? new Date(Number(nanos) / 1e6)
      : typeof value === 'number'
        ? new Date(value)
        : new Date(String(value))
  return Number.isFinite(date.valueOf()) ? date : undefined
}

export function LogListPanel({ rows }: DashboardPanelRendererProps) {
  return (
    <ul className="max-h-80 divide-y divide-border overflow-auto">
      {rows.map((row, index) => {
        const timestamp = logTime(row)
        const traceID = String(columnValue(row, ['trace_id']) ?? '')
        const content = (
          <>
            <div className="mt-0.5 shrink-0">
              <LogLevelBadge severity={row.severity ?? row.level} />
            </div>
            <div className="min-w-0">
              <p className="break-words font-mono text-[11px] leading-5">
                {formatValue(columnValue(row, ['body', 'message', 'name']))}
              </p>
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 font-mono text-[10px] text-muted-foreground">
                <span>{formatValue(columnValue(row, ['service_name', 'service']))}</span>
                {timestamp ? (
                  <time dateTime={timestamp.toISOString()} title={timestamp.toISOString()}>
                    {timestamp.toLocaleString(undefined, {
                      year: 'numeric',
                      month: 'short',
                      day: 'numeric',
                      hour: '2-digit',
                      minute: '2-digit',
                      second: '2-digit',
                    })}
                  </time>
                ) : (
                  <span>Timestamp unavailable</span>
                )}
              </div>
            </div>
          </>
        )
        const target = traceID ? `/logs?traceId=${encodeURIComponent(traceID)}` : '/logs'
        return (
          <li key={index}>
            <Link
              to={target}
              className="flex gap-2 px-1 py-2.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50"
            >
              {content}
            </Link>
          </li>
        )
      })}
    </ul>
  )
}
