import { formatValue, numberValue } from './format'
import type { DashboardPanelRendererProps } from './types'

export function HeatmapPanel({ rows }: DashboardPanelRendererProps) {
  const values = rows.map(row => numberValue(row.value) ?? 0)
  const max = Math.max(...values, 1)
  const firstBucket = formatValue(rows[0]?.x ?? rows[0]?.group_value)
  const lastBucket = formatValue(rows[rows.length - 1]?.x ?? rows[rows.length - 1]?.group_value)
  return <div className="rounded-md border border-border bg-muted/30 p-3"><div className="grid grid-cols-8 gap-1.5 sm:grid-cols-10">{rows.map((row, index) => {
    const value = values[index]
    const intensityPercent = Math.round(100 * Math.sqrt(value / max))
    return <div key={index} title={`${formatValue(row.x ?? row.group_value)}: ${formatValue(value)}`} className="aspect-square min-h-7 rounded-sm transition-colors" style={{ backgroundColor: `color-mix(in srgb, var(--heatmap-high) ${intensityPercent}%, var(--heatmap-low))` }} />
  })}</div><div className="mt-3 flex justify-between font-mono text-[10px] text-muted-foreground"><span>Bucket {firstBucket}</span><span>Bucket {lastBucket}</span></div><div className="mt-2 flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground"><span>Frequency</span><i aria-hidden="true" className="h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: 'var(--heatmap-low)' }}/><span>lower</span><i aria-hidden="true" className="ml-1 h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: 'var(--heatmap-high)' }}/><span>higher</span></div></div>
}
