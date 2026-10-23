import { useEffect, useRef, useState } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
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

export function TimeSeriesPanel({
  rows,
  panel,
  annotations = [],
}: DashboardPanelRendererProps & { annotations?: DashboardPanelRendererProps['rows'] }) {
  let settings: {
    scroll?: boolean
    max_height_px?: number
    min_width_px?: number
    time_window_seconds?: number
  } = {}
  try {
    settings = JSON.parse(panel.settings_json) as typeof settings
  } catch {
    /* optional settings */
  }
  const [now, setNow] = useState(Date.now)
  const timeWindow = settings.time_window_seconds
  useEffect(() => {
    if (!timeWindow || timeWindow <= 0) return
    const timer = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(timer)
  }, [timeWindow])
  const containerRef = useRef<HTMLDivElement>(null)
  const [containerWidth, setContainerWidth] = useState(720)
  useEffect(() => {
    const element = containerRef.current
    if (!element || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width > 0) setContainerWidth(entry.contentRect.width)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [rows.length])
  const series = points(rows)
  if (!series.length) return null
  const minWidth = settings.min_width_px ?? 0
  const width = Math.max(containerWidth, minWidth, 1)
  const height = 210
  const left = 88
  const right = 14
  const top = 18
  const bottom = 40
  const min = Math.min(...series.map((point) => point.value))
  const max = Math.max(...series.map((point) => point.value))
  const range = max - min || 1
  const maxTime = timeWindow ? now * 1e6 : Math.max(...series.map((point) => point.time))
  const minTime = timeWindow
    ? maxTime - timeWindow * 1e9
    : Math.min(...series.map((point) => point.time))
  const timeRange = maxTime - minTime || 1
  const grouped = new Map<string, Point[]>()
  for (const point of series) grouped.set(point.group, [...(grouped.get(point.group) ?? []), point])
  const colors = ['#315b7d', '#8b5cf6', '#d97706', '#0f766e', '#dc2626']
  const coordinate = (point: Point) => ({
    x: left + ((point.time - minTime) / timeRange) * (width - left - right),
    y: height - bottom - ((point.value - min) / range) * (height - top - bottom),
  })
  const yTicks = [max, min + range / 2, min]
  const xTicks = [minTime, minTime + timeRange / 2, maxTime].map(timeLabel)

  // Match SVG units to the viewport so axis text keeps its screen font size.
  const scroll = settings.scroll ?? minWidth > 0
  return (
    <div ref={containerRef} className="min-w-0">
      <div
        className={scroll ? 'overflow-auto' : ''}
        style={scroll ? { maxHeight: settings.max_height_px ?? 360 } : undefined}
      >
        <svg
          viewBox={`0 0 ${width} ${height}`}
          style={minWidth ? { minWidth } : undefined}
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
          {annotations.map((event, index) => {
            const time = numberValue(event.timestamp ?? event.timestamp_ns ?? event.time)
            if (time === undefined || time < minTime || time > maxTime) return null
            const x = left + ((time - minTime) / timeRange) * (width - left - right)
            const label = formatValue(
              event.label ?? event.release ?? event.version ?? event.name ?? 'Release',
            )
            return (
              <g key={`${time}-${index}`}>
                <line
                  x1={x}
                  y1={top}
                  x2={x}
                  y2={height - bottom}
                  stroke="#d97706"
                  strokeWidth="1.5"
                  strokeDasharray="4 3"
                />
                <circle cx={x} cy={top + 5} r="4" fill="#d97706">
                  <title>{`${label} · ${timeLabel(time)}`}</title>
                </circle>
              </g>
            )
          })}
          {yTicks.map((value, index) => (
            <text
              key={index}
              x={left - 8}
              y={top + (index * (height - top - bottom)) / 2 + 3}
              textAnchor="end"
              className="fill-muted-foreground font-mono text-base"
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
              className="fill-muted-foreground font-mono text-base"
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
      </div>
      {grouped.size > 1 && (
        <div
          className="mt-2 max-h-24 space-y-1 overflow-auto"
          role="region"
          tabIndex={0}
          aria-label={`${panel.title} legend`}
        >
          {[...grouped.keys()].map((group, index) => (
            <span
              key={group}
              className="flex min-w-0 items-center gap-1 font-mono text-xs text-muted-foreground"
            >
              <i
                className="h-2 w-2 shrink-0 rounded-full"
                style={{ backgroundColor: colors[index % colors.length] }}
              />
              <Tooltip>
                <TooltipTrigger className="min-w-0 flex-1 truncate text-left">
                  {group}
                </TooltipTrigger>
                <TooltipContent
                  role="tooltip"
                  className="max-w-[min(38rem,calc(100vw-2rem))] break-all"
                >
                  {group}
                </TooltipContent>
              </Tooltip>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
