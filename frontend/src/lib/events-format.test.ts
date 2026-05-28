import { describe, it, expect } from 'vitest'
import { fmtRelMs } from './events-format'

describe('fmtRelMs', () => {
  it('returns +0ms for an event at the span start', () => {
    expect(fmtRelMs(0, 0)).toBe('+0ms')
    expect(fmtRelMs(1_000_000_000, 1_000_000_000)).toBe('+0ms')
  })

  it('formats sub-microsecond deltas as nanoseconds', () => {
    expect(fmtRelMs(500, 0)).toBe('+500ns')
  })

  it('uses microseconds only for deltas under 100 µs', () => {
    expect(fmtRelMs(2_500, 0)).toBe('+2.5µs')
    expect(fmtRelMs(50_000, 0)).toBe('+50.0µs')
  })

  it('switches to milliseconds at 100 µs to match the mockup ("+0.4ms")', () => {
    expect(fmtRelMs(400_000, 0)).toBe('+0.4ms')
    expect(fmtRelMs(6_100_000, 0)).toBe('+6.1ms')
    expect(fmtRelMs(7_800_000, 0)).toBe('+7.8ms')
  })

  it('preserves negative deltas with a leading minus sign', () => {
    expect(fmtRelMs(0, 5_000_000)).toBe('-5.0ms')
    expect(fmtRelMs(0, 1_500)).toBe('-1.5µs')
  })

  it('uses the span start, not absolute time, as the anchor', () => {
    expect(fmtRelMs(1_006_100_000, 1_000_000_000)).toBe('+6.1ms')
  })
})
