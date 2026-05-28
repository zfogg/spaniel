import { describe, it, expect, vi, afterEach } from 'vitest'
import { fmtRelative } from './fmt-relative'

const NOW_MS = 1_700_000_000_000

afterEach(() => vi.useRealTimers())

function nowNs() { return NOW_MS * 1_000_000 }

describe('fmtRelative', () => {
  it('returns "just now" for sub-second differences', () => {
    vi.setSystemTime(NOW_MS + 500)
    expect(fmtRelative(nowNs())).toBe('just now')
  })

  it('returns "Xs ago" for differences under 60s', () => {
    vi.setSystemTime(NOW_MS + 12_000)
    expect(fmtRelative(nowNs())).toBe('12s ago')
  })

  it('returns "Xm ago" for differences under 60 minutes', () => {
    vi.setSystemTime(NOW_MS + 4 * 60 * 1_000)
    expect(fmtRelative(nowNs())).toBe('4m ago')
  })

  it('returns "Xh ago" for differences of hours', () => {
    vi.setSystemTime(NOW_MS + 3 * 60 * 60 * 1_000)
    expect(fmtRelative(nowNs())).toBe('3h ago')
  })

  it('floors to whole units (59s stays 59s, not 1m)', () => {
    vi.setSystemTime(NOW_MS + 59_999)
    expect(fmtRelative(nowNs())).toBe('59s ago')
  })
})
