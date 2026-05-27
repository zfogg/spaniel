import type { MetricSeries } from '@/lib/api'
import type { BucketedSeries } from '@/lib/metrics-bucket'

/**
 * fmtVal renders a metric value with unit-aware formatting. Percentages get
 * a 0-decimal % suffix; dollars get a $ prefix with thousands separators;
 * everything else uses the same magnitude-based rounding as the chart axis
 * labels (k for thousands, integers for >=100, one decimal for >=10, etc.).
 */
export function fmtVal(v: number, unit: string): string {
  if (!Number.isFinite(v)) return '—'
  if (unit === '%') return `${Math.round(v * 100)}%`
  if (unit === '$') return `$${Math.round(v).toLocaleString()}`
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}k`
  if (Math.abs(v) >= 100) return v.toFixed(0)
  if (Math.abs(v) >= 10) return v.toFixed(1)
  return v.toFixed(2)
}

export interface Stat {
  label: string
  value: string
  sub?: string
  tone?: 'ok' | 'danger'
}

/**
 * statsFor builds the four-up stat strip shown above the chart. The boxes
 * differ by metric type:
 *   - gauge: now / 5m ago / min / max
 *   - counter: rate-per-min / total / peak / unit
 *   - histogram: p50 / p95 / p99 / max p99
 * tone is 'danger' for negative trends (gauge ↓, p95>300ms, p99>600ms).
 */
export function statsFor(m: MetricSeries, b: BucketedSeries): Stat[] {
  if (m.type === 'gauge') {
    const s = b.values
    const last = s[s.length - 1] ?? 0
    const prev = s[Math.max(0, s.length - 6)] ?? 0
    const delta = last - prev
    return [
      { label: 'now', value: fmtVal(last, m.unit), sub: 'latest sample' },
      {
        label: '5m ago',
        value: fmtVal(prev, m.unit),
        sub: `${delta >= 0 ? '+' : ''}${fmtVal(delta, m.unit)} vs now`,
        tone: delta < 0 ? 'danger' : 'ok',
      },
      { label: 'min', value: fmtVal(Math.min(...s), m.unit), sub: 'window' },
      { label: 'max', value: fmtVal(Math.max(...s), m.unit), sub: 'window' },
    ]
  }
  if (m.type === 'counter') {
    const s = b.values
    const total = s.reduce((a, c) => a + c, 0)
    const rate = s[s.length - 1] ?? 0
    const peakIdx = s.indexOf(Math.max(...s))
    return [
      { label: 'rate / min', value: fmtVal(rate, m.unit), sub: 'last sample' },
      { label: 'total', value: fmtVal(total, m.unit), sub: 'window' },
      { label: 'peak', value: fmtVal(s[peakIdx] ?? 0, m.unit), sub: `idx ${peakIdx}` },
      { label: 'unit', value: m.unit || '—', sub: 'cumulative' },
    ]
  }
  // histogram
  const p50 = b.p50 ?? []
  const p95 = b.p95 ?? []
  const p99 = b.p99 ?? []
  const p95Last = p95[p95.length - 1] ?? 0
  const p99Last = p99[p99.length - 1] ?? 0
  return [
    { label: 'p50', value: fmtVal(p50[p50.length - 1] ?? 0, m.unit), sub: 'latest' },
    { label: 'p95', value: fmtVal(p95Last, m.unit), sub: 'latest', tone: p95Last > 300 ? 'danger' : undefined },
    { label: 'p99', value: fmtVal(p99Last, m.unit), sub: 'latest', tone: p99Last > 600 ? 'danger' : undefined },
    { label: 'max p99', value: fmtVal(p99.length ? Math.max(...p99) : 0, m.unit), sub: 'window' },
  ]
}
