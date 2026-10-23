import { formatValue, numberValue } from './format'
import type { DashboardPanelRendererProps } from './types'

const colors = ['#3b82f6', '#10b981', '#f59e0b', '#a855f7', '#f43f5e', '#06b6d4']
function settings(raw: string): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(raw)
    return value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

export function CategoryPanel({ panel, rows }: DashboardPanelRendererProps) {
  const config = settings(panel.settings_json)
  const unit = typeof config.unit === 'string' ? config.unit : ''
  const data = rows.flatMap((row) => {
    const value = numberValue(row.value)
    return value === undefined ? [] : [{ label: formatValue(row.label), value }]
  })
  const pie = panel.display_type === 'pie'
  if (!data.length) return <p role="status">No numeric values to display.</p>
  if (pie && data.some(({ value }) => value < 0))
    return <p role="alert">Pie panels require non-negative values.</p>
  const scale = Math.max(...data.map(({ value }) => Math.abs(value)), 1)
  // Normalize first so even large finite values cannot overflow the total.
  const total = data.reduce((sum, { value }) => sum + value / scale, 0)
  if (pie && total === 0) return <p role="status">No positive values to display.</p>
  const min = Math.min(0, ...data.map(({ value }) => value / scale))
  const max = Math.max(0, ...data.map(({ value }) => value / scale))
  const range = max - min || 1
  return (
    <div className="flex flex-wrap items-center gap-4" data-testid={`${panel.display_type}-panel`}>
      {pie && (
        <svg
          role="img"
          aria-label={`${panel.title} pie chart`}
          viewBox="0 0 120 120"
          className="h-44 w-44 max-w-full shrink-0"
        >
          {data.map(({ label, value }, index) => {
            const fraction = value / scale / total
            const start = data
              .slice(0, index)
              .reduce((sum, row) => sum + row.value / scale / total, 0)
            return (
              <circle
                key={index}
                cx="60"
                cy="60"
                r="25"
                fill="none"
                stroke={colors[index % colors.length]}
                strokeWidth="50"
                pathLength="1"
                strokeDasharray={`${fraction} ${1 - fraction}`}
                strokeDashoffset={-start}
                transform="rotate(-90 60 60)"
              >
                <title>
                  {label}: {formatValue(value)} {unit} ({formatValue(fraction * 100)}%)
                </title>
              </circle>
            )
          })}
        </svg>
      )}
      <ul className="min-w-0 basis-32 flex-1 space-y-2" aria-label={`${panel.title} values`}>
        {data.map(({ label, value }, index) => (
          <li key={index} className="text-xs">
            <div className="flex flex-wrap justify-between gap-3">
              <span className="break-all">{label}</span>
              <span className="shrink-0 font-mono">
                {formatValue(value)} {unit}
                {pie ? ` (${formatValue((value / scale / total) * 100)}%)` : ''}
              </span>
            </div>
            {!pie && (
              <svg
                role="img"
                aria-label={`${label}: ${value} ${unit}`}
                viewBox="0 0 100 6"
                preserveAspectRatio="none"
                className="mt-1 h-3 w-full rounded bg-muted"
              >
                <rect
                  x={((Math.min(0, value / scale) - min) / range) * 100}
                  y="0"
                  width={(Math.abs(value / scale) / range) * 100}
                  height="6"
                  fill={colors[index % colors.length]}
                />
                <line
                  x1={(-min / range) * 100}
                  x2={(-min / range) * 100}
                  y1="0"
                  y2="6"
                  stroke="currentColor"
                  strokeWidth="0.3"
                />
              </svg>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

export function GaugePanel({ panel, rows }: DashboardPanelRendererProps) {
  const config = settings(panel.settings_json)
  const value = numberValue(rows[0]?.value)
  const min = numberValue(config.min) ?? 0
  const max = numberValue(config.max) ?? 100
  const unit = typeof config.unit === 'string' ? config.unit : ''
  if (max <= min || !Number.isFinite(max - min))
    return <p role="alert">Gauge maximum must be greater than minimum and define a finite range.</p>
  if (value === undefined) return <p role="status">No numeric value to display.</p>
  const fraction = Math.max(0, Math.min(1, (value - min) / (max - min)))
  return (
    <div className="mx-auto max-w-xs text-center" data-testid="gauge-panel">
      <svg
        role="img"
        aria-label={`${panel.title}: ${formatValue(value)} ${unit}; range ${min} to ${max}`}
        viewBox="0 0 200 115"
        className="w-full"
      >
        <path
          d="M 20 95 A 80 80 0 0 1 180 95"
          fill="none"
          stroke="currentColor"
          strokeOpacity="0.15"
          strokeWidth="16"
        />
        <path
          d="M 20 95 A 80 80 0 0 1 180 95"
          fill="none"
          stroke={colors[0]}
          strokeWidth="16"
          pathLength="1"
          strokeDasharray={`${fraction} 1`}
        />
        <text x="100" y="90" textAnchor="middle" fill="currentColor" fontSize="24">
          {formatValue(value)} {unit}
        </text>
      </svg>
      <div className="flex justify-between text-xs text-muted-foreground">
        <span>{formatValue(min)}</span>
        <span>{formatValue(max)}</span>
      </div>
      {(value < min || value > max) && (
        <p className="text-xs">Value is outside the configured range.</p>
      )}
    </div>
  )
}
