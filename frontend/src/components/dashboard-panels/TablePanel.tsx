import { durationLabel, formatValue, timeLabel } from './format'
import { Link } from 'react-router-dom'
import type { DashboardPanelRendererProps } from './types'

function columnLabel(key: string) {
  if (key === 'duration_sec') return 'Duration Sec'
  if (key === 'http_status') return 'HTTP Status'
  return key.replace(/_/g, ' ')
}

function tableValue(key: string, value: unknown) {
  if (
    /(^|_)(duration|latency)(?:_ns)?$/i.test(key) ||
    (/_ns$/i.test(key) && /duration|latency/i.test(key))
  )
    return durationLabel(value)
  if (/timestamp|time(?:_ns)?$|started_at|ended_at/i.test(key)) return timeLabel(value)
  return formatValue(value, key === 'duration_sec' ? 3 : 2)
}

export function TablePanel({ rows, columns }: DashboardPanelRendererProps) {
  const keys = columns.length ? columns : Object.keys(rows[0] ?? {})
  if (!keys.length) return null
  return (
    <div className="max-h-80 overflow-auto">
      <table className="w-full text-left text-xs">
        <thead className="sticky top-0 z-10 bg-muted/95 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
          <tr>
            {keys.map((key) => (
              <th key={key} className="whitespace-nowrap px-3 py-2 font-medium">
                {columnLabel(key)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {rows.map((row, index) => (
            <tr key={index} className="hover:bg-muted/50">
              {keys.map((key) => {
                const value = tableValue(key, row[key])
                return (
                  <td
                    key={key}
                    title={value}
                    className="max-w-64 truncate px-3 py-2 font-mono text-[11px] tabular-nums"
                  >
                    {key === 'trace_id' && row[key] ? (
                      <Link
                        to={`/traces/${encodeURIComponent(String(row[key]))}`}
                        className="text-accent-ink underline-offset-2 hover:underline focus-visible:underline"
                      >
                        {value}
                      </Link>
                    ) : (
                      value
                    )}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
