import { describe, it, expect } from 'vitest'
import { diffStatusFor, layoutSpan, columnWindow, sharedWindowNs } from './diff-layout'
import type { Span } from './api'

function s(o: Partial<Span> & { span_id: string }): Span {
  return {
    trace_id: 't', span_id: o.span_id, parent_span_id: '',
    service_name: o.service_name ?? 'svc',
    name: o.name ?? 'op',
    kind: 1,
    start_ns: o.start_ns ?? 0,
    end_ns: o.end_ns ?? 0,
    duration_ns: o.duration_ns ?? 0,
    status_code: 1, status_message: '',
    attributes: '{}', resource: '{}',
    session_id: '', session_label: '', received_at: 0,
    events: [],
    links: [],
  } as Span
}

describe('diffStatusFor', () => {
  const diffSpans = [
    { service_name: 'api',     name: 'GET /cart',     status: 'unchanged', delta_pct: 0 },
    { service_name: 'pricing', name: 'PriceQuote',    status: 'changed',   delta_pct: 12.5 },
    { service_name: 'pricing', name: 'cache.miss',    status: 'added',     delta_pct: 0 },
    { service_name: 'cart',    name: 'discount.apply',status: 'removed',   delta_pct: 0 },
  ]

  it('returns "unchanged" when every span on both sides matches', () => {
    // Diffing a session against itself → every (svc,name) maps to unchanged.
    const status = diffStatusFor(s({ span_id: 'a', service_name: 'api', name: 'GET /cart' }), diffSpans)
    expect(status).toBe('unchanged')
  })

  it('returns "added" for a span only present in compare', () => {
    expect(diffStatusFor(s({ span_id: 'b', service_name: 'pricing', name: 'cache.miss' }), diffSpans))
      .toBe('added')
  })

  it('returns "removed" for a span only present in baseline', () => {
    expect(diffStatusFor(s({ span_id: 'c', service_name: 'cart', name: 'discount.apply' }), diffSpans))
      .toBe('removed')
  })

  it('returns "changed" when the matched DiffSpan reports a delta', () => {
    expect(diffStatusFor(s({ span_id: 'd', service_name: 'pricing', name: 'PriceQuote' }), diffSpans))
      .toBe('changed')
  })

  it('falls back to "unchanged" for a span the diff knows nothing about', () => {
    expect(diffStatusFor(s({ span_id: 'e', service_name: 'mystery', name: 'op' }), diffSpans))
      .toBe('unchanged')
  })

  it('treats unknown status strings as "unchanged" (forward-compat)', () => {
    expect(diffStatusFor(s({ span_id: 'f', service_name: 'api', name: 'GET /cart' }),
      [{ service_name: 'api', name: 'GET /cart', status: 'something-new', delta_pct: 0 }],
    )).toBe('unchanged')
  })
})

describe('layoutSpan', () => {
  it('positions a span at the start of the window at 0%', () => {
    const { leftPct, widthPct } = layoutSpan(s({ span_id: 'a', start_ns: 0, duration_ns: 100, end_ns: 100 }), 0, 100)
    expect(leftPct).toBe(0)
    expect(widthPct).toBe(100)
  })

  it('positions a mid-window span proportionally', () => {
    const { leftPct, widthPct } = layoutSpan(s({ span_id: 'a', start_ns: 25, duration_ns: 50, end_ns: 75 }), 0, 100)
    expect(leftPct).toBe(25)
    expect(widthPct).toBe(50)
  })

  it('clamps a zero-duration span to a minimum visible width', () => {
    const { widthPct } = layoutSpan(s({ span_id: 'a', start_ns: 50, duration_ns: 0, end_ns: 50 }), 0, 100)
    expect(widthPct).toBeGreaterThanOrEqual(0.6)
  })

  it('caps widthPct at 100 even when duration exceeds the window', () => {
    const { widthPct } = layoutSpan(s({ span_id: 'a', start_ns: 0, duration_ns: 500, end_ns: 500 }), 0, 100)
    expect(widthPct).toBe(100)
  })

  it('clamps a negative leftPct (span before window start) to 0', () => {
    const { leftPct } = layoutSpan(s({ span_id: 'a', start_ns: -100, duration_ns: 50, end_ns: -50 }), 0, 100)
    expect(leftPct).toBe(0)
  })

  it('returns 0/100 for a zero-window column', () => {
    const { leftPct, widthPct } = layoutSpan(s({ span_id: 'a' }), 0, 0)
    expect(leftPct).toBe(0)
    expect(widthPct).toBe(100)
  })
})

describe('columnWindow', () => {
  it('returns {0,0} for an empty column', () => {
    expect(columnWindow([])).toEqual({ startNs: 0, windowNs: 0 })
  })

  it('uses min(start) → max(end) across the spans', () => {
    const spans = [
      s({ span_id: 'a', start_ns: 100, end_ns: 200 }),
      s({ span_id: 'b', start_ns:  50, end_ns: 180 }),
      s({ span_id: 'c', start_ns: 150, end_ns: 320 }),
    ]
    expect(columnWindow(spans)).toEqual({ startNs: 50, windowNs: 270 })
  })
})

describe('sharedWindowNs', () => {
  it('picks the larger of the two column windows so neither overflows', () => {
    const baseline = [s({ span_id: 'a', start_ns: 0, end_ns: 100 })]      // 100ns
    const compare  = [s({ span_id: 'b', start_ns: 0, end_ns: 250 })]      // 250ns
    expect(sharedWindowNs(baseline, compare)).toBe(250)
  })

  it('returns 0 when both sides are empty', () => {
    expect(sharedWindowNs([], [])).toBe(0)
  })
})
