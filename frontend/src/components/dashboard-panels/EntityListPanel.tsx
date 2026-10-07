import { columnValue, formatValue, numberValue } from './format'
import { Link } from 'react-router-dom'
import type { DashboardPanelRendererProps } from './types'

function tone(status: string) {
  if (/error|alert|fail|critical/i.test(status)) return 'bg-danger/15 text-danger'
  if (/slow|warn|degrad/i.test(status)) return 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
  return 'bg-[var(--ok-bg)] text-[var(--ok-ink)]'
}

type EntitySettings = { columns?: { label?: string; value?: string; secondary?: string; badge?: string; link?: string }; unit?: string }

function settings(raw: string): EntitySettings {
  try { return JSON.parse(raw) as EntitySettings } catch { return {} }
}

export function EntityListPanel({ rows, panel }: DashboardPanelRendererProps) {
  const config = settings(panel.settings_json)
  const columns = config.columns ?? {}
  const values = rows.map(row => numberValue(columnValue(row, [columns.value ?? 'primary_value', 'value', 'duration_ns'])) ?? 0)
  const max = Math.max(...values, 1)
  return <ul className="divide-y divide-border">
    {rows.map((row, index) => {
      const label = formatValue(columnValue(row, [columns.label ?? 'label', 'service_name', 'name']))
      const value = values[index]
      const secondary = formatValue(columnValue(row, [columns.secondary ?? '__none__'], '__none__'))
      const status = String(columnValue(row, [columns.badge ?? 'status', 'state'], '') ?? '')
      const href = columns.link ? String(row[columns.link] ?? '') : ''
      const labelNode = href ? <Link to={href} className="truncate font-medium text-accent-ink hover:underline">{label}</Link> : <span className="truncate font-medium">{label}</span>
      return <li key={index} className="grid grid-cols-[minmax(0,1fr)_80px_auto_auto] items-center gap-3 py-2.5 text-xs">
        <span className="min-w-0">{labelNode}{secondary !== '—' && secondary !== '__none__' && <small className="block truncate text-[10px] text-muted-foreground">{secondary}</small>}</span>
        <span className="h-1.5 overflow-hidden rounded-full bg-muted"><i className="block h-full rounded-full bg-accent" style={{ width: `${Math.max(4, value / max * 100)}%` }}/></span>
        <span className="font-mono text-[10px] tabular-nums">{formatValue(value)}{config.unit ? ` ${config.unit}` : ''}</span>
        {status && <span className={`rounded px-1.5 py-0.5 font-mono text-[9px] ${tone(status)}`}>{status}</span>}
      </li>
    })}
  </ul>
}
