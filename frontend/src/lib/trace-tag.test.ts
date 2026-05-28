import { describe, it, expect } from 'vitest'
import { traceTag } from './trace-tag'

function make(overrides: Partial<Parameters<typeof traceTag>[0]> = {}) {
  return {
    has_n1: false,
    duration_ns: 100_000_000,
    status_code: 1,
    session_id: 'sess-a',
    ...overrides,
  }
}

describe('traceTag', () => {
  it('returns n+1 when has_n1 is true', () => {
    expect(traceTag(make({ has_n1: true }), null)).toBe('n+1')
  })

  it('n+1 wins over slow', () => {
    expect(traceTag(make({ has_n1: true, duration_ns: 300_000_000 }), null)).toBe('n+1')
  })

  it('returns slow when duration > 250ms', () => {
    expect(traceTag(make({ duration_ns: 250_000_001 }), null)).toBe('slow')
  })

  it('returns null when duration is exactly 250ms', () => {
    expect(traceTag(make({ duration_ns: 250_000_000 }), null)).toBeNull()
  })

  it('returns baseline when session matches the baseline session id', () => {
    expect(traceTag(make({ session_id: 'base-1' }), 'base-1')).toBe('baseline')
  })

  it('slow wins over baseline', () => {
    expect(traceTag(make({ duration_ns: 300_000_000, session_id: 'base-1' }), 'base-1')).toBe('slow')
  })

  it('returns error when status_code is 2', () => {
    expect(traceTag(make({ status_code: 2 }), null)).toBe('error')
  })

  it('baseline wins over error', () => {
    expect(traceTag(make({ status_code: 2, session_id: 'base-1' }), 'base-1')).toBe('baseline')
  })

  it('returns null when nothing matches', () => {
    expect(traceTag(make(), null)).toBeNull()
  })
})
