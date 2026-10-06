import type { PanelRow } from './types'

export function valueOf(row: PanelRow | undefined, key = 'value'): unknown {
  return row?.[key]
}

export function formatValue(value: unknown, maximumFractionDigits = 2): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'number') return Number.isFinite(value) ? value.toLocaleString(undefined, { maximumFractionDigits }) : '—'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return String(value)
}

export function numberValue(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

export function durationLabel(value: unknown): string {
  const ns = numberValue(value)
  if (ns === undefined) return formatValue(value)
  return Math.abs(ns) >= 1_000_000_000 ? `${formatValue(ns / 1_000_000_000)} s` : `${formatValue(ns / 1_000_000)} ms`
}

export function columnValue(row: PanelRow, preferred: string[], fallback = 'value'): unknown {
  for (const key of preferred) if (key in row) return row[key]
  return row[fallback]
}

export function timeLabel(value: unknown): string {
  const timestamp = numberValue(value)
  if (!timestamp) return formatValue(value)
  const date = new Date(timestamp > 1e14 ? timestamp / 1_000_000 : timestamp)
  return Number.isNaN(date.valueOf()) ? formatValue(value) : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
