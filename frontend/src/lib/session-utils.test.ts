import { describe, it, expect } from 'vitest'
import { isBranchLabel, fmtSessionSize, fmtP95 } from './session-utils'

describe('isBranchLabel', () => {
  it('returns true for labels containing /', () => {
    expect(isBranchLabel('foo/bar')).toBe(true)
    expect(isBranchLabel('feat/checkout')).toBe(true)
    expect(isBranchLabel('hotfix/redis-pool')).toBe(true)
  })

  it('returns false for labels without /', () => {
    expect(isBranchLabel('scratch')).toBe(false)
    expect(isBranchLabel('quick-debug')).toBe(false)
    expect(isBranchLabel('main')).toBe(false)
    expect(isBranchLabel('')).toBe(false)
  })
})

describe('fmtSessionSize', () => {
  it('formats zero/negative as dash', () => {
    expect(fmtSessionSize(0)).toBe('—')
    expect(fmtSessionSize(-1)).toBe('—')
  })

  it('formats bytes', () => {
    expect(fmtSessionSize(512)).toBe('512 B')
  })

  it('formats kilobytes', () => {
    expect(fmtSessionSize(1024)).toBe('1 KB')
    expect(fmtSessionSize(10 * 1024)).toBe('10 KB')
  })

  it('formats megabytes', () => {
    expect(fmtSessionSize(48 * 1_048_576)).toBe('48 MB')
  })

  it('formats gigabytes', () => {
    expect(fmtSessionSize(1.5 * 1_073_741_824)).toBe('1.5 GB')
  })
})

describe('fmtP95', () => {
  it('formats zero/negative as dash', () => {
    expect(fmtP95(0)).toBe('—')
    expect(fmtP95(-1)).toBe('—')
  })

  it('formats microseconds', () => {
    expect(fmtP95(500)).toBe('1µs')   // 0.5µs rounds to 1 with toFixed(0)
    expect(fmtP95(50_000)).toBe('50µs')
  })

  it('formats milliseconds', () => {
    expect(fmtP95(498_000_000)).toBe('498ms')
    expect(fmtP95(612_000_000)).toBe('612ms')
  })

  it('formats seconds for values >= 1000ms', () => {
    expect(fmtP95(2_000_000_000)).toBe('2.0s')
  })
})
