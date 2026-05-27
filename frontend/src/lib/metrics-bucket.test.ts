import { describe, it, expect } from 'vitest'
import { bucketPoints } from './metrics-bucket'
import type { MetricSeriesPoint } from '@/lib/api'

function pt(ts: number, v: number, percentile?: 'p50' | 'p95' | 'p99'): MetricSeriesPoint {
  return percentile ? { timestamp_ns: ts, value: v, percentile } : { timestamp_ns: ts, value: v }
}

describe('bucketPoints', () => {
  it('returns zeros when input is empty', () => {
    const r = bucketPoints([], 'gauge', 10)
    expect(r.values).toHaveLength(10)
    expect(r.values.every(v => v === 0)).toBe(true)
  })

  it('places each point into its time bin', () => {
    // 4 points spread across the window — at the start, 1/3, 2/3, and end.
    const pts: MetricSeriesPoint[] = [
      pt(0, 1),
      pt(30, 2),
      pt(60, 3),
      pt(90, 4),
    ]
    const r = bucketPoints(pts, 'gauge', 4)
    expect(r.values).toEqual([1, 2, 3, 4])
  })

  it('forward-fills empty bins with the previous value', () => {
    // 2 points only — bin 0 = 5, bin 1 = 0 should forward-fill to 5,
    // bin 2 = 9, bin 3 inherits 9.
    const pts: MetricSeriesPoint[] = [pt(0, 5), pt(60, 9)]
    const r = bucketPoints(pts, 'counter', 4)
    expect(r.values).toEqual([5, 5, 5, 9])
  })

  it('splits histogram points by percentile', () => {
    const pts: MetricSeriesPoint[] = [
      pt(0,  10, 'p50'), pt(0,  30, 'p95'), pt(0,  60, 'p99'),
      pt(90, 12, 'p50'), pt(90, 40, 'p95'), pt(90, 80, 'p99'),
    ]
    const r = bucketPoints(pts, 'histogram', 4)
    expect(r.p50).toEqual([10, 10, 10, 12])
    expect(r.p95).toEqual([30, 30, 30, 40])
    expect(r.p99).toEqual([60, 60, 60, 80])
    // values is aliased to p95 so the sparkline reflects the worst-case line.
    expect(r.values).toBe(r.p95)
  })

  it('clamps points past tMax into the last bin', () => {
    // Edge case: a point exactly at tMax must not produce an out-of-bounds idx.
    const pts: MetricSeriesPoint[] = [pt(0, 1), pt(100, 9)]
    const r = bucketPoints(pts, 'gauge', 3)
    expect(r.values[r.values.length - 1]).toBe(9)
  })
})
