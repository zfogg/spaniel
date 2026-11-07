import { formatValue, numberValue, timeLabel, valueOf } from './format'
import type { DashboardPanelRendererProps } from './types'

export function SingleValuePanel({ rows, panel, history = [] }: DashboardPanelRendererProps) {
  const current = valueOf(rows[0])
  const number = numberValue(current)
  let showHistory = false
  try {
    showHistory = JSON.parse(panel.settings_json).show_history === true
  } catch {
    // Optional settings do not prevent the current value from rendering.
  }
  const points = history.filter(
    (point) => Number.isFinite(point.value) && Number.isFinite(point.timestamp_ms),
  )
  // Include the live value between persisted sampling intervals without writing
  // every WebSocket update into the database.
  if (points.length && number !== undefined) {
    points.push({
      timestamp_ms: Math.max(Date.now(), points[points.length - 1].timestamp_ms + 1),
      value: number,
    })
  }
  const formatExtremum = (value: number) => {
    const magnitude = Math.abs(value)
    if (magnitude >= 1e9 || (magnitude > 0 && magnitude < 1e-4)) return value.toExponential(3)
    return value.toLocaleString(undefined, { maximumSignificantDigits: 8 })
  }
  const min = points.length ? Math.min(...points.map((point) => point.value)) : 0
  const max = points.length ? Math.max(...points.map((point) => point.value)) : 0
  // Normalize first to avoid overflow for very large or tiny finite values.
  const scale = Math.max(Math.abs(min), Math.abs(max), Number.MIN_VALUE)
  const normalizedMin = min / scale
  const normalizedMax = max / scale
  const padding = max === min ? 0.1 : (normalizedMax - normalizedMin) * 0.12
  const low = normalizedMin - padding
  const high = normalizedMax + padding
  const start = points[0]?.timestamp_ms ?? 0
  const end = points[points.length - 1]?.timestamp_ms ?? start
  const coords = points
    .map((point, index) => {
      const x =
        end === start
          ? points.length === 1
            ? 50
            : (index * 100) / (points.length - 1)
          : ((point.timestamp_ms - start) * 100) / (end - start)
      return `${x},${100 - ((point.value / scale - low) * 100) / (high - low)}`
    })
    .join(' ')
  const secondary =
    rows.length > 1 ? `${rows.length.toLocaleString()} groups returned` : 'Current value'
  return (
    <div className="flex min-h-16 flex-col justify-center px-1 py-3">
      <data value={number} className="font-mono text-3xl font-semibold tracking-tight tabular-nums">
        {formatValue(current)}
      </data>
      <p className="mt-2 text-xs text-muted-foreground">{secondary}</p>
      {showHistory && (
        <div className="mt-3" aria-label="Value history">
          {points.length ? (
            <>
              <div className="text-right font-mono text-[10px] tabular-nums text-muted-foreground">
                max {formatExtremum(max)}
              </div>
              <svg
                viewBox="0 0 100 100"
                preserveAspectRatio="none"
                className="h-14 w-full overflow-visible"
                role="img"
                aria-label={`Value history, minimum ${formatExtremum(min)}, maximum ${formatExtremum(max)}`}
              >
                {points.length === 1 ? (
                  <circle cx="50" cy="50" r="2" fill="currentColor" className="text-primary" />
                ) : (
                  <polyline
                    points={coords}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    vectorEffect="non-scaling-stroke"
                    className="text-primary"
                  />
                )}
              </svg>
              <div className="flex justify-between gap-2 font-mono text-[10px] tabular-nums text-muted-foreground">
                <span>
                  {timeLabel(start)} – {timeLabel(end)}
                </span>
                <span>min {formatExtremum(min)}</span>
              </div>
            </>
          ) : (
            <p className="text-xs text-muted-foreground">
              {number === undefined ? 'History requires a numeric value.' : 'Waiting for history…'}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
