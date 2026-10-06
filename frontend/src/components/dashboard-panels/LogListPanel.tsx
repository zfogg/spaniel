import { formatValue, columnValue } from './format'
import { Link } from 'react-router-dom'
import { logSeverityLabel } from '@/lib/log-severity'
import type { DashboardPanelRendererProps } from './types'

function severityTone(value: unknown) {
  const severity = Number(value)
  if (severity >= 17) return 'bg-danger/15 text-danger'
  if (severity >= 13) return 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
  return 'bg-muted text-muted-foreground'
}

export function LogListPanel({ rows }: DashboardPanelRendererProps) {
  return <ul className="max-h-80 divide-y divide-border overflow-auto">{rows.map((row, index) => {
    const severity = columnValue(row, ['severity', 'level'])
    const traceID = String(columnValue(row, ['trace_id']) ?? '')
    const content = <><span className={`mt-0.5 shrink-0 rounded px-1.5 py-0.5 font-mono text-[9px] font-semibold ${severityTone(severity)}`}>{logSeverityLabel(severity)}</span><div className="min-w-0"><p className="break-words font-mono text-[11px] leading-5">{formatValue(columnValue(row, ['body', 'message', 'name']))}</p><p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{formatValue(columnValue(row, ['service_name', 'service']))}</p></div></>
    const target = traceID ? `/logs?traceId=${encodeURIComponent(traceID)}` : '/logs'
    return <li key={index}><Link to={target} className="flex gap-2 px-1 py-2.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50">{content}</Link></li>
  })}</ul>
}
