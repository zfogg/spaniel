import { formatValue, numberValue, valueOf } from './format'
import type { DashboardPanelRendererProps } from './types'

export function SingleValuePanel({ rows }: DashboardPanelRendererProps) {
  const current = valueOf(rows[0])
  const number = numberValue(current)
  const secondary =
    rows.length > 1 ? `${rows.length.toLocaleString()} groups returned` : 'Current value'
  return (
    <div className="flex min-h-32 flex-col justify-center px-1 py-3">
      <data value={number} className="font-mono text-3xl font-semibold tracking-tight tabular-nums">
        {formatValue(current)}
      </data>
      <p className="mt-2 text-xs text-muted-foreground">{secondary}</p>
    </div>
  )
}
