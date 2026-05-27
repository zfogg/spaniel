import { describe, it, expect } from 'vitest'
import { fmtVal, statsFor } from './metrics-format'
import type { MetricSeries } from '@/lib/api'
import type { BucketedSeries } from '@/lib/metrics-bucket'

describe('fmtVal', () => {
  it('formats percentages with 0 decimals and a % suffix', () => {
    expect(fmtVal(0.5, '%')).toBe('50%')
    expect(fmtVal(0.876, '%')).toBe('88%')
  })
  it('formats dollars with thousands separators', () => {
    expect(fmtVal(1234, '$')).toBe('$1,234')
    expect(fmtVal(9, '$')).toBe('$9')
  })
  it('uses k for thousands+', () => {
    expect(fmtVal(1500, 'req')).toBe('1.5k')
    expect(fmtVal(42_000, 'req')).toBe('42.0k')
  })
  it('uses integer for >=100, 1 decimal for >=10, 2 decimals below', () => {
    expect(fmtVal(120, 'ms')).toBe('120')
    expect(fmtVal(42.7, 'ms')).toBe('42.7')
    expect(fmtVal(3.14, 'ms')).toBe('3.14')
  })
  it('handles non-finite values gracefully', () => {
    expect(fmtVal(NaN, 'req')).toBe('—')
    expect(fmtVal(Infinity, 'req')).toBe('—')
  })
})

function mkSeries(type: MetricSeries['type'], unit = 'req'): MetricSeries {
  return {
    name: 'test',
    service_name: 'svc',
    type,
    unit,
    description: '',
    points: [],
  }
}

describe('statsFor — gauge', () => {
  it('returns now / 5m ago / min / max', () => {
    const m = mkSeries('gauge')
    const b: BucketedSeries = { values: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] }
    const s = statsFor(m, b)
    expect(s.map(x => x.label)).toEqual(['now', '5m ago', 'min', 'max'])
    expect(s[0].value).toBe('10.0')             // last
    expect(s[1].value).toBe('5.00')             // 6 from the end (idx 4 in a len-10 array)
    expect(s[2].value).toBe('1.00')             // min
    expect(s[3].value).toBe('10.0')             // max
  })
  it('flags a downward trend as danger', () => {
    const m = mkSeries('gauge')
    // last=2, prev (idx 0)=10 → delta=-8 → tone=danger.
    const b: BucketedSeries = { values: [10, 9, 8, 7, 6, 2] }
    const s = statsFor(m, b)
    expect(s[1].tone).toBe('danger')
    expect(s[1].sub).toContain('-8')
  })
  it('flags a flat or upward trend as ok', () => {
    const m = mkSeries('gauge')
    const b: BucketedSeries = { values: [1, 2, 3, 4, 5, 6] }
    const s = statsFor(m, b)
    expect(s[1].tone).toBe('ok')
  })
})

describe('statsFor — counter', () => {
  it('returns rate / total / peak / unit', () => {
    const m = mkSeries('counter', 'req')
    const b: BucketedSeries = { values: [1, 2, 9, 3, 5] }
    const s = statsFor(m, b)
    expect(s.map(x => x.label)).toEqual(['rate / min', 'total', 'peak', 'unit'])
    expect(s[0].value).toBe('5.00')                       // last sample
    expect(s[1].value).toBe('20.0')                       // sum
    expect(s[2].value).toBe('9.00')                       // peak
    expect(s[2].sub).toBe('idx 2')                        // peak idx
    expect(s[3].value).toBe('req')                        // unit pass-through
  })
  it('falls back to "—" when unit is empty', () => {
    const m = mkSeries('counter', '')
    const b: BucketedSeries = { values: [1, 2] }
    expect(statsFor(m, b)[3].value).toBe('—')
  })
})

describe('statsFor — histogram', () => {
  it('returns p50 / p95 / p99 / max p99', () => {
    const m = mkSeries('histogram', 'ms')
    const b: BucketedSeries = {
      values: [],
      p50: [10, 12, 14],
      p95: [80, 90, 100],
      p99: [120, 140, 160],
    }
    const s = statsFor(m, b)
    expect(s.map(x => x.label)).toEqual(['p50', 'p95', 'p99', 'max p99'])
    expect(s[0].value).toBe('14.0')   // p50 latest
    expect(s[1].value).toBe('100')    // p95 latest
    expect(s[2].value).toBe('160')    // p99 latest
    expect(s[3].value).toBe('160')    // max p99 in window
  })
  it('flags p95 > 300ms and p99 > 600ms as danger', () => {
    const m = mkSeries('histogram', 'ms')
    const b: BucketedSeries = {
      values: [],
      p50: [10],
      p95: [350],
      p99: [800],
    }
    const s = statsFor(m, b)
    expect(s[1].tone).toBe('danger')   // p95 > 300
    expect(s[2].tone).toBe('danger')   // p99 > 600
  })
  it('does not flag healthy histograms', () => {
    const m = mkSeries('histogram', 'ms')
    const b: BucketedSeries = {
      values: [],
      p50: [5],
      p95: [50],
      p99: [120],
    }
    const s = statsFor(m, b)
    expect(s[1].tone).toBeUndefined()
    expect(s[2].tone).toBeUndefined()
  })
})
