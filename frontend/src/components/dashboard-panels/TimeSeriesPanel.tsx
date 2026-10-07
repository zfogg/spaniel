import { formatValue, numberValue, timeLabel } from './format'
import type { DashboardPanelRendererProps } from './types'

type Point = { time: number; value: number; label: string; group: string }

function points(rows: DashboardPanelRendererProps['rows']): Point[] {
  return rows
    .flatMap((row, index) => {
      const value = numberValue(row.value)
      const timestamp = numberValue(
        row.timestamp_ns ?? row.time_ns ?? row.timestamp ?? row.start_ns,
      )
      if (value === undefined) return []
      return [
        {
          time: timestamp ?? index,
          value,
          label: timeLabel(timestamp ?? index),
          group: formatValue(row.group_value ?? 'value'),
        },
      ]
    })
    .sort((a, b) => a.time - b.time)
}

export function TimeSeriesPanel({ rows }: DashboardPanelRendererProps) {
  const series = points(rows)
  if (!series.length) return null
  const width = 720
  const height = 210
  const left = 48
  const right = 14
  const top = 12
  const bottom = 30
  const min = Math.min(...series.map((point) => point.value))
  const max = Math.max(...series.map((point) => point.value))
  const range = max - min || 1
  const minTime = Math.min(...series.map((point) => point.time))
  const maxTime = Math.max(...series.map((point) => point.time))
  const timeRange = maxTime - minTime || 1
  const grouped = new Map<string, Point[]>()
  for (const point of series) grouped.set(point.group, [...(grouped.get(point.group) ?? []), point])
  const colors = ['#315b7d', '#8b5cf6', '#d97706', '#0f766e', '#dc2626']
  const coordinate = (point: Point) => ({
    x: left + ((point.time - minTime) / timeRange) * (width - left - right),
    y: height - bottom - ((point.value - min) / range) * (height - top - bottom),
  })
  const yTicks = [max, min + range / 2, min]
  const xTicks = [
    series[0].label,
    series[Math.floor((series.length - 1) / 2)]?.label ?? series[0].label,
    series[series.length - 1].label,
  ]
  return (
    <div className="min-w-0">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="h-52 w-full"
        role="img"
        aria-label={`Time series from ${formatValue(min)} to ${formatValue(max)}`}
      >
        <g className="stroke-border">
          {yTicks.map((_, index) => {
            const y = top + (index * (height - top - bottom)) / 2
            return (
              <line
                key={y}
                x1={left}
                y1={y}
                x2={width - right}
                y2={y}
                strokeDasharray={index === 2 ? undefined : '3 4'}
              />
            )
          })}
          <line x1={left} y1={top} x2={left} y2={height - bottom} />
          <line x1={left} y1={height - bottom} x2={width - right} y2={height - bottom} />
        </g>
        {yTicks.map((value, index) => (
          <text
            key={index}
            x={left - 8}
            y={top + (index * (height - top - bottom)) / 2 + 3}
            textAnchor="end"
            className="fill-muted-foreground font-mono text-[10px]"
          >
            {formatValue(value)}
          </text>
        ))}
        {xTicks.map((label, index) => (
          <text
            key={`${label}-${index}`}
            x={left + (index * (width - left - right)) / 2}
            y={height - 9}
            textAnchor={index === 0 ? 'start' : index === 2 ? 'end' : 'middle'}
            className="fill-muted-foreground font-mono text-[10px]"
          >
            {label}
          </text>
        ))}
        {[...grouped.entries()].map(([group, groupPoints], groupIndex) => (
          <g key={group}>
            <polyline
              points={groupPoints
                .map((point) => {
                  const { x, y } = coordinate(point)
                  return `${x},${y}`
                })
                .join(' ')}
              fill="none"
              stroke={colors[groupIndex % colors.length]}
              strokeWidth="2.5"
              strokeLinejoin="round"
              strokeLinecap="round"
            />
            {groupPoints.map((point, index) => {
              const { x, y } = coordinate(point)
              return (
                <circle
                  key={`${point.time}-${index}`}
                  cx={x}
                  cy={y}
                  r="3"
                  fill={colors[groupIndex % colors.length]}
                >
                  <title>{`${group} · ${point.label}: ${formatValue(point.value)}`}</title>
                </circle>
              )
            })}
          </g>
        ))}
      </svg>
      {grouped.size > 1 && (
        <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1">
          {[...grouped.keys()].map((group, index) => (
            <span
              key={group}
              className="flex items-center gap-1 font-mono text-[10px] text-muted-foreground"
            >
              <i
                className="h-2 w-2 rounded-full"
                style={{ backgroundColor: colors[index % colors.length] }}
              />
              {group}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
