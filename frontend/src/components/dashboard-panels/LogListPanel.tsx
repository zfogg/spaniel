import { formatValue, columnValue } from './format'
import { Link } from 'react-router-dom'
import { logSeverityLabel } from '@/lib/log-severity'
import type { DashboardPanelRendererProps } from './types'

function severityLabel(value: unknown) {
  if (typeof value === 'string' && /^(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)$/i.test(value.trim())) {
    return value.trim().toUpperCase().replace('WARNING', 'WARN')
  }
  return value == null || value === '' ? 'UNKNOWN' : logSeverityLabel(value)
}

function severityTone(level: string) {
  switch (level) {
    case 'ERROR': return 'bg-red-500/15 text-red-700 dark:text-red-300'
    case 'FATAL': return 'bg-purple-500/15 text-purple-700 dark:text-purple-300'
    case 'WARN': return 'bg-yellow-500/15 text-yellow-800 dark:text-yellow-300'
    case 'INFO': return 'bg-blue-500/15 text-blue-700 dark:text-blue-300'
    default: return 'bg-muted text-muted-foreground'
  }
}

function logTime(row: Record<string, unknown>): Date | undefined {
  const nanos = row.timestamp_ns ?? row.time_ns
  const value = nanos ?? row.timestamp ?? row.time
  if (value == null || value === '') return undefined
  const date = nanos != null ? new Date(Number(nanos) / 1e6) : typeof value === 'number' ? new Date(value) : new Date(String(value))
  return Number.isFinite(date.valueOf()) ? date : undefined
}

export function LogListPanel({ rows }: DashboardPanelRendererProps) {
  return <ul className="max-h-80 divide-y divide-border overflow-auto">{rows.map((row, index) => {
    const severity = severityLabel(row.severity ?? row.level)
    const timestamp = logTime(row)
    const traceID = String(columnValue(row, ['trace_id']) ?? '')
    const content = <><span className={`mt-0.5 h-fit shrink-0 rounded px-1.5 py-0.5 font-mono text-[9px] font-semibold ${severityTone(severity)}`}>{severity}</span><div className="min-w-0"><p className="break-words font-mono text-[11px] leading-5">{formatValue(columnValue(row, ['body', 'message', 'name']))}</p><div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 font-mono text-[10px] text-muted-foreground"><span>{formatValue(columnValue(row, ['service_name', 'service']))}</span>{timestamp ? <time dateTime={timestamp.toISOString()} title={timestamp.toISOString()}>{timestamp.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</time> : <span>Timestamp unavailable</span>}</div></div></>
    const target = traceID ? `/logs?traceId=${encodeURIComponent(traceID)}` : '/logs'
    return <li key={index}><Link to={target} className="flex gap-2 px-1 py-2.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50">{content}</Link></li>
  })}</ul>
}
