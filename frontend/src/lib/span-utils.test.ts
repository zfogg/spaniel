import { describe, it, expect } from 'vitest'
import { fmtNs, flatten, buildTagMap, svcColor, SPAN_PALETTE } from './span-utils'
import type { Span, LintWarning } from './api'

// ── fmtNs ─────────────────────────────────────────────────────────────────────

describe('fmtNs', () => {
  it('formats nanoseconds under 1µs', () => {
    expect(fmtNs(0)).toBe('0ns')
    expect(fmtNs(1)).toBe('1ns')
    expect(fmtNs(999)).toBe('999ns')
  })

  it('formats microseconds', () => {
    expect(fmtNs(1_000)).toBe('1.0µs')
    expect(fmtNs(1_500)).toBe('1.5µs')
    expect(fmtNs(999_999)).toBe('1000.0µs')
  })

  it('formats milliseconds', () => {
    expect(fmtNs(1_000_000)).toBe('1.0ms')
    expect(fmtNs(612_000_000)).toBe('612.0ms')
    expect(fmtNs(999_999_999)).toBe('1000.0ms')
  })

  it('formats seconds', () => {
    expect(fmtNs(1_000_000_000)).toBe('1.00s')
    expect(fmtNs(1_840_000_000)).toBe('1.84s')
  })
})

// ── flatten ───────────────────────────────────────────────────────────────────

function makeSpan(id: string, parentId = ''): Span {
  return {
    trace_id: 'trace-1', span_id: id, parent_span_id: parentId,
    service_name: 'svc', name: id, kind: 1,
    start_ns: 0, end_ns: 100, duration_ns: 100,
    status_code: 0, status_message: '',
    attributes: '{}', resource: '{}',
    session_id: '', session_label: '', received_at: 0,
    events: [], links: [],
  }
}

describe('flatten', () => {
  it('returns empty array for empty input', () => {
    expect(flatten([])).toEqual([])
  })

  it('handles a single root span', () => {
    const result = flatten([makeSpan('root')])
    expect(result).toHaveLength(1)
    expect(result[0].span.span_id).toBe('root')
    expect(result[0].depth).toBe(0)
    expect(result[0].orphan).toBe(false)
  })

  it('places children after parent in DFS order', () => {
    const spans = [makeSpan('root'), makeSpan('child', 'root'), makeSpan('grandchild', 'child')]
    const result = flatten(spans)
    expect(result.map(f => f.span.span_id)).toEqual(['root', 'child', 'grandchild'])
  })

  it('assigns correct depths', () => {
    const spans = [makeSpan('root'), makeSpan('child', 'root'), makeSpan('grandchild', 'child')]
    const result = flatten(spans)
    expect(result[0].depth).toBe(0)
    expect(result[1].depth).toBe(1)
    expect(result[2].depth).toBe(2)
  })

  it('marks spans with unknown parent as orphans', () => {
    const orphan = makeSpan('orphan', 'nonexistent-parent')
    const result = flatten([orphan])
    expect(result[0].orphan).toBe(true)
  })

  it('does not mark spans with zero parent ID as orphans', () => {
    const span = makeSpan('root', '0000000000000000')
    const result = flatten([span])
    expect(result[0].orphan).toBe(false)
    expect(result[0].depth).toBe(0)
  })

  it('handles sibling spans at same depth', () => {
    const spans = [
      makeSpan('root'),
      makeSpan('child-a', 'root'),
      makeSpan('child-b', 'root'),
    ]
    const result = flatten(spans)
    expect(result).toHaveLength(3)
    const depths = result.map(f => f.depth)
    expect(depths[0]).toBe(0)
    expect(depths[1]).toBe(1)
    expect(depths[2]).toBe(1)
  })

  it('handles multiple root spans', () => {
    const spans = [makeSpan('root-a'), makeSpan('root-b')]
    const result = flatten(spans)
    expect(result).toHaveLength(2)
    expect(result.every(f => f.depth === 0)).toBe(true)
    expect(result.every(f => !f.orphan)).toBe(true)
  })
})

// ── buildTagMap ───────────────────────────────────────────────────────────────

function makeLint(spanId: string, ruleId: string): LintWarning {
  return {
    span_id: spanId, trace_id: 'trace-1', session_id: '',
    rule_id: ruleId, message: 'msg', severity: 'warn', created_at: 0,
  }
}

describe('buildTagMap', () => {
  it('returns empty map for no warnings', () => {
    expect(buildTagMap([])).toEqual(new Map())
  })

  it('maps span_id to "lint" for a regular lint warning', () => {
    const map = buildTagMap([makeLint('span-1', 'HTTP.MISSING_METHOD')])
    expect(map.get('span-1')).toBe('lint')
  })

  it('maps span_id to "n+1" when rule_id contains N+1', () => {
    const map = buildTagMap([makeLint('span-2', 'PERF.N+1')])
    expect(map.get('span-2')).toBe('n+1')
  })

  it('first warning wins for a span with multiple warnings', () => {
    const warnings = [
      makeLint('span-3', 'HTTP.MISSING_METHOD'),
      makeLint('span-3', 'PERF.N+1'),
    ]
    const map = buildTagMap(warnings)
    expect(map.get('span-3')).toBe('lint')
  })

  it('handles multiple distinct spans', () => {
    const warnings = [
      makeLint('s1', 'HTTP.MISSING_METHOD'),
      makeLint('s2', 'PERF.N+1'),
      makeLint('s3', 'DB.MISSING_SYSTEM'),
    ]
    const map = buildTagMap(warnings)
    expect(map.get('s1')).toBe('lint')
    expect(map.get('s2')).toBe('n+1')
    expect(map.get('s3')).toBe('lint')
  })
})

// ── svcColor ──────────────────────────────────────────────────────────────────

describe('svcColor', () => {
  it('returns a color from the palette', () => {
    const { fg } = svcColor('api-gateway')
    expect(SPAN_PALETTE).toContain(fg)
  })

  it('is deterministic — same input gives same output', () => {
    expect(svcColor('cart-service')).toEqual(svcColor('cart-service'))
  })

  it('different service names can produce different colors', () => {
    const colors = new Set(SPAN_PALETTE.map((_, i) => svcColor(`service-${i}`).fg))
    expect(colors.size).toBeGreaterThan(1)
  })

  it('bg is fg with "28" alpha suffix', () => {
    const { fg, bg } = svcColor('payment-service')
    expect(bg).toBe(fg + '28')
  })
})

describe('hasLinks', () => {
  it('returns false for null / undefined / empty', async () => {
    const { hasLinks } = await import('./span-utils')
    expect(hasLinks(null)).toBe(false)
    expect(hasLinks(undefined)).toBe(false)
    expect(hasLinks({} as { links?: never[] })).toBe(false)
    expect(hasLinks({ links: [] })).toBe(false)
  })
  it('returns true when at least one link is present', async () => {
    const { hasLinks } = await import('./span-utils')
    expect(hasLinks({ links: [{ linked_trace_id: 'x' }] })).toBe(true)
    expect(hasLinks({ links: [{ linked_trace_id: 'x' }, { linked_trace_id: 'y' }] })).toBe(true)
  })
})

describe('shortTraceId', () => {
  it('passes through short IDs untouched', async () => {
    const { shortTraceId } = await import('./span-utils')
    expect(shortTraceId('')).toBe('')
    expect(shortTraceId('abcd')).toBe('abcd')
  })
  it('abbreviates long IDs as first4…last5', async () => {
    const { shortTraceId } = await import('./span-utils')
    expect(shortTraceId('9ef357237e8c74950f711b77779d19eb')).toBe('9ef3…d19eb')
  })
})
