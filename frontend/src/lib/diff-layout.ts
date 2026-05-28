// Per-span layout + status helpers for the side-by-side SessionDiff
// mockup. The chart renders proportional bars positioned by `leftPct` /
// `widthPct` (percent of the visible time window) and tinted by the
// `status` returned from the DiffResult.

import type { Span } from '@/lib/api'

export type DiffStatus = 'added' | 'removed' | 'changed' | 'unchanged'

export interface DiffSpanLite {
  name: string
  service_name: string
  status: string
  delta_pct: number
}

// diffStatusFor — match a raw Span back to its DiffSpan by (service, name).
// DiffSpan doesn't carry span_id so this is the only join key the server
// gives us. Unknown spans → 'unchanged' (the safe default — they get the
// neutral bar treatment).
export function diffStatusFor(
  span: Pick<Span, 'service_name' | 'name'>,
  diffSpans: DiffSpanLite[],
): DiffStatus {
  for (const d of diffSpans) {
    if (d.service_name === span.service_name && d.name === span.name) {
      const s = d.status as DiffStatus
      if (s === 'added' || s === 'removed' || s === 'changed' || s === 'unchanged') {
        return s
      }
    }
  }
  return 'unchanged'
}

// layoutSpan — proportional bar position for a span on a column whose
// visible window is `[startNs, startNs + windowNs]`. Width clamps to a
// visible minimum so zero-duration spans remain hittable.
export interface SpanLayout {
  leftPct: number
  widthPct: number
}

export function layoutSpan(
  span: Pick<Span, 'start_ns' | 'duration_ns'>,
  startNs: number,
  windowNs: number,
): SpanLayout {
  if (windowNs <= 0) return { leftPct: 0, widthPct: 100 }
  const leftPct = ((span.start_ns - startNs) / windowNs) * 100
  const widthPct = Math.max(0.6, (span.duration_ns / windowNs) * 100)
  return {
    leftPct: clampPct(leftPct),
    widthPct: Math.min(100, widthPct),
  }
}

function clampPct(v: number): number {
  if (!Number.isFinite(v)) return 0
  if (v < 0) return 0
  if (v > 100) return 100
  return v
}

// columnWindow — pick the [start, span] pair for the visible bars in one
// column. Empty spans → 0/0 (the SpanColumn renders an empty state).
export function columnWindow(spans: Pick<Span, 'start_ns' | 'end_ns'>[]): { startNs: number; windowNs: number } {
  if (spans.length === 0) return { startNs: 0, windowNs: 0 }
  let lo = spans[0].start_ns
  let hi = spans[0].end_ns
  for (const s of spans) {
    if (s.start_ns < lo) lo = s.start_ns
    if (s.end_ns > hi) hi = s.end_ns
  }
  return { startNs: lo, windowNs: Math.max(0, hi - lo) }
}

// sharedWindowNs — the scale used by BOTH columns so a 100ms span looks
// the same width on either side. Returns the larger of the two column
// windows so neither bar overflows.
export function sharedWindowNs(
  baseline: Pick<Span, 'start_ns' | 'end_ns'>[],
  compare: Pick<Span, 'start_ns' | 'end_ns'>[],
): number {
  const a = columnWindow(baseline).windowNs
  const b = columnWindow(compare).windowNs
  return Math.max(a, b)
}
