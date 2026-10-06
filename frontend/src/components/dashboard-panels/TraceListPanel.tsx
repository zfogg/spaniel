import { formatValue, columnValue } from './format'
import { Link } from 'react-router-dom'
import type { DashboardPanelRendererProps } from './types'

function durationLabel(value: unknown) {
  const ns = Number(value)
  if (!Number.isFinite(ns)) return formatValue(value)
  if (ns >= 1_000_000_000) return `${(ns / 1_000_000_000).toLocaleString(undefined, { maximumFractionDigits: 2 })} s`
  return `${(ns / 1_000_000).toLocaleString(undefined, { maximumFractionDigits: 2 })} ms`
}

export function TraceListPanel({ rows }: DashboardPanelRendererProps) {
  return <div className="overflow-hidden"><div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 border-b border-border bg-muted/60 px-1 py-1.5 font-mono text-[9px] uppercase tracking-wide text-muted-foreground"><span>Trace / service</span><span>Duration</span></div><ul className="max-h-72 divide-y divide-border overflow-auto">{rows.map((row, index) => {
    const service = formatValue(columnValue(row, ['service_name', 'service']))
    const name = formatValue(columnValue(row, ['name', 'trace_id']))
    const duration = durationLabel(columnValue(row, ['duration_ns', 'duration']))
    const traceID = String(columnValue(row, ['trace_id']) ?? '')
    const content = <><div className="min-w-0"><p className="truncate text-xs font-medium">{name}</p><p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">{service}</p></div><span className="self-center font-mono text-[10px] tabular-nums text-muted-foreground">{duration}</span></>
    return <li key={index}>{traceID ? <Link to={`/traces/${encodeURIComponent(traceID)}`} className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 px-1 py-2.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50">{content}</Link> : <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 px-1 py-2.5">{content}</div>}</li>
  })}</ul></div>
}
