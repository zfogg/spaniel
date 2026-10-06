import { formatValue, numberValue } from './format'
import type { DashboardPanelRendererProps } from './types'

export function HeatmapPanel({ rows }: DashboardPanelRendererProps) {
  const values = rows.map(row => numberValue(row.value) ?? 0)
  const max = Math.max(...values, 1)
  return <div className="rounded-md border border-border bg-muted/30 p-3"><div className="grid grid-cols-8 gap-1.5 sm:grid-cols-10">{rows.map((row, index) => {
    const value = values[index]
    const opacity = 0.13 + 0.87 * Math.sqrt(value / max)
    return <div key={index} title={`${formatValue(row.group_value)}: ${formatValue(value)}`} className="aspect-square min-h-7 rounded-sm bg-accent transition-opacity" style={{ opacity }} />
  })}</div><div className="mt-3 flex justify-between font-mono text-[10px] text-muted-foreground"><span>Lower volume</span><span>Higher volume</span></div></div>
}
