import { describe, it, expect } from 'vitest'
import { xPosForTrace, valueAtBin } from './metrics-correlate'
import type { MetricSeriesPoint, TraceOverlay } from './api'

const pt = (ts: number): MetricSeriesPoint => ({ timestamp_ns: ts, value: 0 })
const trace = (start: number): Pick<TraceOverlay, 'start_ns'> => ({ start_ns: start })

describe('xPosForTrace', () => {
  const points = [pt(1000), pt(2000), pt(3000)]
  const BINS = 60

  it('maps an exact midpoint trace to the middle bin', () => {
    expect(xPosForTrace(trace(2000), points, BINS)).toBe(Math.round(0.5 * (BINS - 1)))
  })

  it('maps the first point to bin 0', () => {
    expect(xPosForTrace(trace(1000), points, BINS)).toBe(0)
  })

  it('maps the last point to bin BINS-1', () => {
    expect(xPosForTrace(trace(3000), points, BINS)).toBe(BINS - 1)
  })

  it('clamps a trace before the first point to bin 0', () => {
    expect(xPosForTrace(trace(500), points, BINS)).toBe(0)
  })

  it('clamps a trace after the last point to bin BINS-1', () => {
    expect(xPosForTrace(trace(99_000), points, BINS)).toBe(BINS - 1)
  })

  it('interpolates a quarter-position trace correctly', () => {
    // 1250 sits 25% of the way from 1000 to 2000, and 12.5% of the way
    // across the whole [1000..3000] window.
    expect(xPosForTrace(trace(1250), points, BINS)).toBe(Math.round(0.125 * (BINS - 1)))
  })

  it('returns null when there are fewer than 2 points', () => {
    expect(xPosForTrace(trace(1000), [pt(1000)], BINS)).toBeNull()
    expect(xPosForTrace(trace(1000), [], BINS)).toBeNull()
  })

  it('returns null when bins is non-positive', () => {
    expect(xPosForTrace(trace(2000), points, 0)).toBeNull()
    expect(xPosForTrace(trace(2000), points, -10)).toBeNull()
  })

  it('returns null when the points span is zero (all same timestamp)', () => {
    expect(xPosForTrace(trace(2000), [pt(1000), pt(1000)], BINS)).toBeNull()
  })

  it('works for any bin count, not just 60', () => {
    expect(xPosForTrace(trace(1000), points, 4)).toBe(0)
    expect(xPosForTrace(trace(3000), points, 4)).toBe(3)
    expect(xPosForTrace(trace(2000), points, 4)).toBe(Math.round(0.5 * 3))
  })
})

describe('valueAtBin', () => {
  it('returns the value at the given bin', () => {
    expect(valueAtBin([10, 20, 30, 40], 2)).toBe(30)
  })

  it('clamps to the first value for negative bins', () => {
    expect(valueAtBin([10, 20, 30], -1)).toBe(10)
  })

  it('clamps to the last value when bin exceeds the array', () => {
    expect(valueAtBin([10, 20, 30], 99)).toBe(30)
  })

  it('returns null when bin is null', () => {
    expect(valueAtBin([10, 20], null)).toBeNull()
  })

  it('returns null on empty input', () => {
    expect(valueAtBin([], 0)).toBeNull()
  })
})
