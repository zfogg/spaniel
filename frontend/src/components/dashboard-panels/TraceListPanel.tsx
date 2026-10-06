import { formatValue, columnValue } from './format'
import type { DashboardPanelRendererProps } from './types'

export function TraceListPanel({ rows }: DashboardPanelRendererProps) {
  return <ul className="max-h-80 divide-y divide-border overflow-auto rounded-md border border-border">{rows.map((row, index) => {
    const service = formatValue(columnValue(row, ['service_name', 'service']))
    const name = formatValue(columnValue(row, ['name', 'trace_id']))
    const duration = formatValue(columnValue(row, ['duration_ns', 'duration']))
    return <li key={index} className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 px-3 py-2.5 hover:bg-muted/50"><div className="min-w-0"><p className="truncate text-xs font-medium">{name}</p><p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">{service}</p></div><span className="self-center font-mono text-[10px] tabular-nums text-muted-foreground">{duration}</span></li>
  })}</ul>
}
