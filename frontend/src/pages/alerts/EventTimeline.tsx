import { type AlertEvent } from '@/lib/api'
import PaginationControls from '@/components/PaginationControls'
import { formatTimestampWithAgo } from './alert-model'
import { TimestampWithAgo } from './AlertTime'
import { useRelativeTimeNow } from './use-relative-time'
import { GroupBadge } from './AlertFields'
export function EventTimeline({
  events,
  total,
  page,
  onPageChange,
  threshold,
  operator,
}: {
  events: AlertEvent[]
  total: number
  page: number
  onPageChange: (page: number) => void
  threshold?: number
  operator?: string
}) {
  const now = useRelativeTimeNow()
  const numericEvents = events
    .slice()
    .reverse()
    .filter((event) => event.value != null)
  const values = numericEvents.map((event) => event.value as number)
  const min = Math.min(...values, threshold ?? 0),
    max = Math.max(...values, threshold ?? 1),
    spread = max - min || 1
  const chart = { width: 720, height: 260, left: 58, right: 18, top: 20, bottom: 42 }
  const plotWidth = chart.width - chart.left - chart.right
  const plotHeight = chart.height - chart.top - chart.bottom
  const yFor = (value: number) => chart.top + (1 - (value - min) / spread) * plotHeight
  const xFor = (index: number) =>
    chart.left +
    (numericEvents.length < 2 ? plotWidth / 2 : (index / (numericEvents.length - 1)) * plotWidth)
  const thresholdY = threshold == null ? null : yFor(threshold)
  const points = values.map((value, index) => `${xFor(index)},${yFor(value)}`).join(' ')
  return events.length ? (
    <div className="space-y-3">
      <div className="rounded border border-border bg-background p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-x-5 gap-y-1 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">Evaluation value history</span>
          <span>
            {numericEvents.length} numeric evaluation{numericEvents.length === 1 ? '' : 's'}
          </span>
        </div>
        <svg
          viewBox={`0 0 ${chart.width} ${chart.height}`}
          className="h-64 w-full"
          role="img"
          aria-label="Alert value history"
        >
          {[0, 0.25, 0.5, 0.75, 1].map((ratio) => {
            const value = max - ratio * spread
            const y = chart.top + ratio * plotHeight
            return (
              <g key={ratio}>
                <line
                  x1={chart.left}
                  x2={chart.width - chart.right}
                  y1={y}
                  y2={y}
                  stroke="var(--line)"
                  strokeOpacity="0.9"
                />
                <text
                  x={chart.left - 10}
                  y={y + 4}
                  textAnchor="end"
                  className="fill-muted-foreground text-[11px]"
                >
                  {value.toLocaleString(undefined, { maximumFractionDigits: 2 })}
                </text>
              </g>
            )
          })}
          {thresholdY != null && (
            <g>
              <line
                x1={chart.left}
                x2={chart.width - chart.right}
                y1={thresholdY}
                y2={thresholdY}
                stroke="var(--danger-ink)"
                strokeWidth="2"
                strokeDasharray="7 5"
              />
              <text
                x={chart.width - chart.right}
                y={Math.max(chart.top + 12, thresholdY - 8)}
                textAnchor="end"
                className="fill-danger-ink text-[11px] font-medium"
              >
                threshold {(threshold ?? 0).toLocaleString()}
              </text>
            </g>
          )}
          {points && (
            <polyline
              fill="none"
              stroke="var(--accent-ink)"
              strokeWidth="4"
              strokeLinejoin="round"
              strokeLinecap="round"
              points={points}
            />
          )}
          {numericEvents.map((event, index) => (
            <circle
              key={event.id}
              cx={xFor(index)}
              cy={yFor(event.value as number)}
              r="4"
              fill="var(--accent-ink)"
            >
              <title>
                {formatTimestampWithAgo(event.created_at, now)}:{' '}
                {(event.value as number).toLocaleString()}
              </title>
            </circle>
          ))}
          {numericEvents.length > 0 && (
            <>
              <text
                x={chart.left}
                y={chart.height - 25}
                textAnchor="start"
                className="fill-muted-foreground text-[11px]"
              >
                {formatTimestampWithAgo(numericEvents[0].created_at, now)}
              </text>
              <text
                x={chart.width - chart.right}
                y={chart.height - 14}
                textAnchor="end"
                className="fill-muted-foreground text-[11px]"
              >
                {formatTimestampWithAgo(numericEvents[numericEvents.length - 1].created_at, now)}
              </text>
            </>
          )}
        </svg>
        <p className="mt-2 text-xs text-muted-foreground">
          Recorded evaluations · min {min.toLocaleString()} · max {max.toLocaleString()}
          {threshold != null ? ` · threshold ${operator ?? ''} ${threshold.toLocaleString()}` : ''}
        </p>
      </div>
      <div className="max-h-48 space-y-2 overflow-auto pr-1">
        {events.map((event) => (
          <div className="rounded border border-border p-2 text-xs" key={event.id}>
            <div className="flex justify-between gap-3">
              <span className="font-medium">{event.kind.split('_').join(' ')}</span>
              <span>
                <TimestampWithAgo nanoseconds={event.created_at} />
              </span>
            </div>
            <p className="mt-1 font-mono text-muted-foreground">
              {event.state} · <GroupBadge groupKey={event.group_key} />
              {event.value != null ? ` · value ${event.value}` : ''}
            </p>
            {event.detail && (
              <p className="mt-1 break-words text-muted-foreground">{event.detail}</p>
            )}
          </div>
        ))}
      </div>
      <PaginationControls
        page={page}
        pageSize={15}
        total={total}
        itemLabel="timeline events"
        onPageChange={onPageChange}
      />
    </div>
  ) : (
    <p className="text-sm text-muted-foreground">No evaluations recorded yet.</p>
  )
}
