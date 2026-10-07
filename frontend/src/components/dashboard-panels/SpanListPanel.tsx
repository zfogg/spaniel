import { Link } from 'react-router-dom'
import { columnValue, formatValue, durationLabel } from './format'
import type { DashboardPanelRendererProps } from './types'

export function SpanListPanel({ rows }: DashboardPanelRendererProps) {
  return (
    <ul className="max-h-80 divide-y divide-border overflow-auto">
      {rows.map((row, index) => {
        const traceID = String(columnValue(row, ['trace_id']) ?? '')
        const name = formatValue(columnValue(row, ['name', 'operation', 'span_id']))
        const service = formatValue(columnValue(row, ['service_name', 'service']))
        const duration = durationLabel(columnValue(row, ['duration_ns', 'duration']))
        const content = (
          <>
            <div className="min-w-0">
              <p className="truncate text-xs font-medium">{name}</p>
              <p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
                {service}
              </p>
            </div>
            <span className="self-center font-mono text-[10px] tabular-nums text-muted-foreground">
              {duration}
            </span>
          </>
        )
        return (
          <li key={index}>
            {traceID ? (
              <Link
                to={`/traces/${encodeURIComponent(traceID)}`}
                className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 px-1 py-2.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50"
              >
                {content}
              </Link>
            ) : (
              <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 px-1 py-2.5">
                {content}
              </div>
            )}
          </li>
        )
      })}
    </ul>
  )
}
