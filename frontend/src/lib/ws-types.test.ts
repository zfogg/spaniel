import { describe, it, expect } from 'vitest'

// Inline the decoder logic (same as what ws.ts does in onmessage)
function decode(raw: string) {
  try {
    const ev = JSON.parse(raw)
    if (typeof ev?.type !== 'string') return null
    return ev
  } catch { return null }
}

describe('WsEvent decoder', () => {
  it('parses a span event', () => {
    const raw = JSON.stringify({ type: 'span', timestamp_ns: 1000, payload: { traceId: 'abc' } })
    const ev = decode(raw)
    expect(ev?.type).toBe('span')
    expect(ev?.payload?.traceId).toBe('abc')
  })

  it('parses a log event', () => {
    const raw = JSON.stringify({ type: 'log', timestamp_ns: 2000, payload: { body: 'hello' } })
    const ev = decode(raw)
    expect(ev?.type).toBe('log')
  })

  it('parses a throughput event', () => {
    const raw = JSON.stringify({ type: 'throughput', timestamp_ns: 3000, payload: { spansPerSec: 4.5, logsPerSec: 1.2 } })
    const ev = decode(raw)
    expect(ev?.payload?.spansPerSec).toBe(4.5)
  })

  it('returns null for invalid JSON', () => {
    expect(decode('not json')).toBeNull()
  })

  it('returns null for missing type field', () => {
    expect(decode(JSON.stringify({ payload: {} }))).toBeNull()
  })

  it('ignores unknown event types gracefully (no throw)', () => {
    const ev = decode(JSON.stringify({ type: 'unknown_future_type', timestamp_ns: 0, payload: {} }))
    expect(ev?.type).toBe('unknown_future_type')
    // Consumer should check type and ignore unknowns — no throw here
  })
})
