import { durationLabel, formatValue, numberValue, timeLabel } from './format'
import type { DashboardPanelRendererProps } from './types'

// SQL contract: timestamp_ns (or x), bucket_ms (or y), value.
// Missing coordinates are distinct from explicit zero counts.
export function HeatmapPanel({ rows }: DashboardPanelRendererProps) {
  const temporal = rows.some(row => row.timestamp_ns != null)
  const duration = rows.some(row => row.bucket_ms != null)
  const points = rows.flatMap(row => {
    const x = row.timestamp_ns ?? row.x ?? row.group_value
    const y = row.bucket_ms ?? row.y ?? 'Frequency'
    const value = numberValue(row.value)
    return x == null || value == null || value < 0 ? [] : [{ x: String(x), y: String(y), value }]
  })
  if (!points.length) return <p className="py-8 text-center text-sm text-muted-foreground">No heatmap data in this range.</p>
  const compare = (a: string, b: string) => {
    const an = numberValue(a), bn = numberValue(b)
    return an !== undefined && bn !== undefined ? an - bn : a.localeCompare(b)
  }
  const xs = [...new Set(points.map(point => point.x))].sort(compare)
  const ys = [...new Set(points.map(point => point.y))].sort(compare).reverse()
  if (xs.length * ys.length > 20000) return <p role="status" className="py-8 text-sm text-muted-foreground">Too many heatmap buckets. Increase the time interval or bucket width in your query.</p>
  const values = new Map<string, Map<string, number>>()
  for (const point of points) {
    const column = values.get(point.x) ?? new Map<string, number>()
    column.set(point.y, (column.get(point.y) ?? 0) + point.value)
    values.set(point.x, column)
  }
  const max = Math.max(1, ...[...values.values()].flatMap(column => [...column.values()]))
  const labelX = (x: string) => temporal ? timeLabel(x) : formatValue(x)
  const labelY = (y: string) => duration ? durationLabel(Number(y) * 1e6) : formatValue(y)
  const cellWidth = Math.max(12, 300 / xs.length), cellHeight = 24
  const width = 80 + xs.length * cellWidth, height = 30 + ys.length * cellHeight
  const ticks = new Set([0, Math.floor((xs.length - 1) / 2), xs.length - 1])
  return <div>
    <div className="max-h-[360px] overflow-auto">
      <svg role="img" aria-label={`Heatmap: ${xs.length} ${temporal ? 'time' : 'x'} buckets by ${ys.length} ${duration ? 'duration' : 'y'} buckets`} viewBox={`0 0 ${width} ${height}`} style={{ minWidth: xs.length > 40 ? width : 0, width: '100%' }} className="block text-muted-foreground">
        {ys.map((y, yi) => <g key={y}>
          <text x={70} y={yi * cellHeight + 16} textAnchor="end" fill="currentColor" fontSize={10}>{labelY(y)}</text>
          {xs.map((x, xi) => {
            const value = values.get(x)?.get(y)
            const intensity = Math.round(100 * Math.sqrt((value ?? 0) / max))
            const detail = `${temporal ? new Date(Number(x) / 1e6).toLocaleString() : labelX(x)} · ${duration ? 'Duration bucket ≥ ' : 'Bucket '}${labelY(y)} · ${value == null ? 'No data' : `Count: ${formatValue(value)}`}`
            return <rect key={x} data-heatmap-cell="" x={80 + xi * cellWidth} y={yi * cellHeight} width={cellWidth - 2} height={cellHeight - 2} rx={2} aria-label={detail} style={{ fill: value == null ? 'var(--muted)' : `color-mix(in srgb, var(--heatmap-high) ${intensity}%, var(--heatmap-low))` }}><title>{detail}</title></rect>
          })}
        </g>)}
        {[...ticks].map(index => <text key={index} x={80 + (index + .5) * cellWidth} y={height - 10} textAnchor={index === 0 ? 'start' : index === xs.length - 1 ? 'end' : 'middle'} fill="currentColor" fontSize={10}>{labelX(xs[index])}</text>)}
      </svg>
    </div>
    <div className="mt-2 flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground"><span>{temporal ? 'Time → · ' : ''}{duration ? 'Duration ↑ · ' : ''}Frequency</span><span>0</span><span className="h-2 w-16 rounded" style={{ background: 'linear-gradient(to right, var(--heatmap-low), var(--heatmap-high))' }}/><span>{formatValue(max)}</span><span className="ml-auto">Color = count · gaps = no data</span></div>
  </div>
}
