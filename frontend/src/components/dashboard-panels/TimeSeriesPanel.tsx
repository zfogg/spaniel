import { formatValue, numberValue, timeLabel } from './format'
import type { DashboardPanelRendererProps } from './types'

type Point = { time: number; value: number; label: string; group: string }

function points(rows: DashboardPanelRendererProps['rows']): Point[] {
  return rows.flatMap((row, index) => {
    const value = numberValue(row.value)
    const timestamp = numberValue(row.timestamp_ns ?? row.time_ns ?? row.timestamp ?? row.start_ns)
    if (value === undefined) return []
    return [{ time: timestamp ?? index, value, label: timeLabel(timestamp ?? index), group: formatValue(row.group_value ?? 'value') }]
  }).sort((a, b) => a.time - b.time)
}

export function TimeSeriesPanel({ rows }: DashboardPanelRendererProps) {
  const series = points(rows)
  if (!series.length) return null
  const width = 720; const height = 210; const inset = 18
  const min = Math.min(...series.map(point => point.value)); const max = Math.max(...series.map(point => point.value)); const range = max - min || 1
  const minTime = Math.min(...series.map(point => point.time)); const maxTime = Math.max(...series.map(point => point.time)); const timeRange = maxTime - minTime || 1
  const grouped = new Map<string, Point[]>()
  for (const point of series) grouped.set(point.group, [...(grouped.get(point.group) ?? []), point])
  const colors = ['#315b7d', '#8b5cf6', '#d97706', '#0f766e', '#dc2626']
  const coordinate = (point: Point) => ({ x: inset + ((point.time - minTime) / timeRange) * (width - inset * 2), y: height - inset - ((point.value - min) / range) * (height - inset * 2) })
  return <div className="rounded-md border border-border bg-muted/20 p-3"><svg viewBox={`0 0 ${width} ${height}`} className="h-52 w-full overflow-visible" role="img" aria-label={`Time series from ${formatValue(min)} to ${formatValue(max)}`}><line x1={inset} y1={height - inset} x2={width - inset} y2={height - inset} className="stroke-border"/><line x1={inset} y1={inset} x2={inset} y2={height - inset} className="stroke-border"/>{[...grouped.entries()].map(([group, groupPoints], groupIndex) => <g key={group}>{<polyline points={groupPoints.map(point => { const { x, y } = coordinate(point); return `${x},${y}` }).join(' ')} fill="none" stroke={colors[groupIndex % colors.length]} strokeWidth="3" strokeLinejoin="round" strokeLinecap="round"/>}{groupPoints.map((point, index) => { const { x, y } = coordinate(point); return <circle key={`${point.time}-${index}`} cx={x} cy={y} r="3.5" fill={colors[groupIndex % colors.length]}><title>{`${group} · ${point.label}: ${formatValue(point.value)}`}</title></circle> })}</g>)}</svg>{grouped.size > 1 && <div className="mb-2 flex flex-wrap gap-x-3 gap-y-1">{[...grouped.keys()].map((group, index) => <span key={group} className="flex items-center gap-1 font-mono text-[10px] text-muted-foreground"><i className="h-2 w-2 rounded-full" style={{ backgroundColor: colors[index % colors.length] }}/>{group}</span>)}</div>}<div className="mt-1 flex justify-between font-mono text-[10px] text-muted-foreground"><span>{series[0].label}</span><span>{series[series.length - 1].label}</span></div></div>
}
