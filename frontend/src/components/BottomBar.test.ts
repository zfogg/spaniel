import { describe, it, expect } from 'vitest'
import { fmtRate } from './BottomBar'

describe('fmtRate', () => {
  it('returns "0 / s" for zero', () => {
    expect(fmtRate(0)).toBe('0 / s')
  })

  it('returns "< 1 / s" for fractional rates', () => {
    expect(fmtRate(0.5)).toBe('< 1 / s')
    expect(fmtRate(0.1)).toBe('< 1 / s')
  })

  it('rounds to nearest integer for rates 1–999', () => {
    expect(fmtRate(12.5)).toBe('13 / s')
    expect(fmtRate(1)).toBe('1 / s')
    expect(fmtRate(999)).toBe('999 / s')
  })

  it('formats rates >= 1000 as k notation', () => {
    expect(fmtRate(1000)).toBe('1.0k / s')
    expect(fmtRate(1200)).toBe('1.2k / s')
    expect(fmtRate(10000)).toBe('10.0k / s')
  })
})
