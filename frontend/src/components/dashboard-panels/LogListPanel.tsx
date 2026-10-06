import { formatValue, columnValue } from './format'
import type { DashboardPanelRendererProps } from './types'

function severityTone(value: unknown) {
  const severity = Number(value)
  if (severity >= 17) return 'bg-danger/15 text-danger'
  if (severity >= 13) return 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
  return 'bg-muted text-muted-foreground'
}

export function LogListPanel({ rows }: DashboardPanelRendererProps) {
  return <ul className="max-h-80 divide-y divide-border overflow-auto rounded-md border border-border">{rows.map((row, index) => {
    const severity = columnValue(row, ['severity', 'level'])
    return <li key={index} className="flex gap-2 px-3 py-2.5 hover:bg-muted/50"><span className={`mt-0.5 shrink-0 rounded px-1.5 py-0.5 font-mono text-[9px] font-semibold ${severityTone(severity)}`}>{formatValue(severity)}</span><div className="min-w-0"><p className="break-words font-mono text-[11px] leading-5">{formatValue(columnValue(row, ['body', 'message', 'name']))}</p><p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{formatValue(columnValue(row, ['service_name', 'service']))}</p></div></li>
  })}</ul>
}
