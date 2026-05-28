import { describe, it, expect } from 'vitest'
import { computeLayout, criticalPath, detectN1SpanIds, n1BannerEntries, n1IssueForSpan } from './trace-waterfall-utils'
import { flatten } from '@/lib/span-utils'
import type { Span, TraceIssue } from '@/lib/api'

const ZERO = '0000000000000000'

function span(o: Partial<Span> & { span_id: string }): Span {
  return {
    trace_id: 't1',
    span_id: o.span_id,
    parent_span_id: o.parent_span_id ?? ZERO,
    service_name: o.service_name ?? 'svc',
    name: o.name ?? 'op',
    kind: o.kind ?? 1,
    start_ns: o.start_ns ?? 0,
    end_ns: o.end_ns ?? 0,
    duration_ns: o.duration_ns ?? 0,
    status_code: o.status_code ?? 1,
    status_message: '',
    attributes: o.attributes ?? '{}',
    resource: '{}',
    session_id: '',
    session_label: '',
    received_at: 0,
    events: o.events ?? [],
    links: o.links ?? [],
  }
}

// ── computeLayout ──────────────────────────────────────────────────────────

describe('computeLayout', () => {
  it('positions a span at depth 0 starting at the trace origin', () => {
    const s = span({ span_id: 'a', start_ns: 0, duration_ns: 100, end_ns: 100 })
    const { leftPct, widthPct } = computeLayout(s, 0, 100)
    expect(leftPct).toBe(0)
    expect(widthPct).toBe(100)
  })

  it('positions a span offset from the trace start', () => {
    const s = span({ span_id: 'a', start_ns: 25, duration_ns: 50, end_ns: 75 })
    const { leftPct, widthPct } = computeLayout(s, 0, 100)
    expect(leftPct).toBe(25)
    expect(widthPct).toBe(50)
  })

  it('clamps width to a visible minimum for zero-duration spans', () => {
    const s = span({ span_id: 'a', start_ns: 50, duration_ns: 0, end_ns: 50 })
    const { widthPct } = computeLayout(s, 0, 100)
    expect(widthPct).toBeGreaterThanOrEqual(0.6)
  })

  it('returns sensible defaults when the trace has zero duration', () => {
    const s = span({ span_id: 'a' })
    const { leftPct, widthPct } = computeLayout(s, 0, 0)
    expect(leftPct).toBe(0)
    expect(widthPct).toBeGreaterThan(0)
  })
})

// ── criticalPath ───────────────────────────────────────────────────────────

describe('criticalPath', () => {
  it('returns an empty set for no spans', () => {
    expect(criticalPath([]).size).toBe(0)
  })

  it('returns just the root when there are no children', () => {
    const path = criticalPath([span({ span_id: 'r', duration_ns: 100, end_ns: 100 })])
    expect([...path]).toEqual(['r'])
  })

  it('follows the longest-duration child at each step', () => {
    const spans = [
      span({ span_id: 'r', duration_ns: 100, end_ns: 100 }),
      span({ span_id: 'a', parent_span_id: 'r', duration_ns: 30, end_ns: 30 }),
      span({ span_id: 'b', parent_span_id: 'r', duration_ns: 80, end_ns: 80 }), // critical
      span({ span_id: 'b1', parent_span_id: 'b', duration_ns: 20, end_ns: 50 }),
      span({ span_id: 'b2', parent_span_id: 'b', duration_ns: 60, end_ns: 75 }), // critical
    ]
    const path = criticalPath(spans)
    expect([...path].sort()).toEqual(['b', 'b2', 'r'])
  })

  it('returns an empty set when no root span is present', () => {
    const orphan = span({ span_id: 'x', parent_span_id: 'missing', duration_ns: 10, end_ns: 10 })
    expect(criticalPath([orphan]).size).toBe(0)
  })
})

// ── N+1 detection ──────────────────────────────────────────────────────────

function dbSpan(id: string, stmt: string, dur = 1_000_000): Span {
  return span({
    span_id: id,
    parent_span_id: 'parent',
    duration_ns: dur,
    end_ns: dur,
    attributes: JSON.stringify({ 'db.statement': stmt, 'db.system': 'postgresql' }),
  })
}

describe('detectN1SpanIds', () => {
  it('flags a server-reported N+1 example span', () => {
    const flat = flatten([span({ span_id: 'parent', duration_ns: 100, end_ns: 100 })])
    const issues: TraceIssue[] = [{
      id: 'i1', trace_id: 't1', session_id: '', kind: 'n_plus_one',
      fingerprint: 'SELECT * FROM users', count: 12, wasted_ns: 5_000_000,
      parent_span_id: 'parent', example_span_id: 'q1', created_at: 0,
    }]
    expect(detectN1SpanIds(flat, issues).has('q1')).toBe(true)
  })

  it('flags all spans in a group of 10+ sharing a db.statement', () => {
    const parent = span({ span_id: 'parent', duration_ns: 100, end_ns: 100 })
    const queries = Array.from({ length: 12 }, (_, i) => dbSpan(`q${i}`, 'SELECT * FROM users'))
    const flat = flatten([parent, ...queries])
    const flagged = detectN1SpanIds(flat, [])
    for (let i = 0; i < 12; i++) expect(flagged.has(`q${i}`)).toBe(true)
  })

  it('does not flag groups smaller than the threshold', () => {
    const parent = span({ span_id: 'parent', duration_ns: 100, end_ns: 100 })
    const queries = Array.from({ length: 5 }, (_, i) => dbSpan(`q${i}`, 'SELECT 1'))
    const flat = flatten([parent, ...queries])
    expect(detectN1SpanIds(flat, []).size).toBe(0)
  })

  it('ignores spans without a db.statement attribute', () => {
    const parent = span({ span_id: 'parent', duration_ns: 100, end_ns: 100 })
    const noop = Array.from({ length: 12 }, (_, i) =>
      span({ span_id: `n${i}`, parent_span_id: 'parent', duration_ns: 1, end_ns: 1, attributes: '{}' }),
    )
    const flat = flatten([parent, ...noop])
    expect(detectN1SpanIds(flat, []).size).toBe(0)
  })
})

describe('n1BannerEntries', () => {
  it('prefers server-side issues when present and sorts by wastedNs desc', () => {
    const issues: TraceIssue[] = [
      { id: 'i1', trace_id: 't1', session_id: '', kind: 'n_plus_one',
        fingerprint: 'SELECT users', count: 12, wasted_ns: 1_000,
        parent_span_id: 'p', example_span_id: 'q1', created_at: 0 },
      { id: 'i2', trace_id: 't1', session_id: '', kind: 'n_plus_one',
        fingerprint: 'SELECT orders', count: 20, wasted_ns: 9_000,
        parent_span_id: 'p', example_span_id: 'q2', created_at: 0 },
    ]
    const entries = n1BannerEntries([], issues)
    expect(entries.map(e => e.fingerprint)).toEqual(['SELECT orders', 'SELECT users'])
  })

  it('derives entries client-side when no issues are reported', () => {
    const parent = span({ span_id: 'p', duration_ns: 100, end_ns: 100 })
    const queries = Array.from({ length: 12 }, (_, i) => dbSpan(`q${i}`, 'SELECT * FROM users', 2_000_000))
    const flat = flatten([parent, ...queries])
    const entries = n1BannerEntries(flat, [])
    expect(entries).toHaveLength(1)
    expect(entries[0].count).toBe(12)
    expect(entries[0].wastedNs).toBe(24_000_000)
  })

  it('returns no entries when groups stay under the threshold', () => {
    const parent = span({ span_id: 'p', duration_ns: 100, end_ns: 100 })
    const queries = Array.from({ length: 9 }, (_, i) => dbSpan(`q${i}`, 'SELECT 1'))
    const flat = flatten([parent, ...queries])
    expect(n1BannerEntries(flat, [])).toEqual([])
  })
})

// ── n1IssueForSpan ─────────────────────────────────────────────────────────

function issue(o: Partial<TraceIssue> & { id: string }): TraceIssue {
  return {
    id: o.id,
    trace_id: o.trace_id ?? 't1',
    session_id: o.session_id ?? '',
    kind: o.kind ?? 'n_plus_one',
    fingerprint: o.fingerprint ?? 'SELECT * FROM t WHERE x=?',
    count: o.count ?? 12,
    wasted_ns: o.wasted_ns ?? 50_000_000,
    parent_span_id: o.parent_span_id ?? '',
    example_span_id: o.example_span_id ?? '',
    created_at: 0,
  }
}

describe('n1IssueForSpan', () => {
  it('returns null when span is null', () => {
    expect(n1IssueForSpan(null, [])).toBeNull()
  })

  it('returns null for non-DB spans (no db.statement attribute)', () => {
    const s = span({ span_id: 'op', parent_span_id: 'p', attributes: '{"http.method":"GET"}' })
    const i = issue({ id: 'i1', parent_span_id: 'p', example_span_id: 'op' })
    expect(n1IssueForSpan(s, [i])).toBeNull()
  })

  it('returns null when no matching n_plus_one issue exists', () => {
    const s = span({ span_id: 'db1', parent_span_id: 'p',
      attributes: '{"db.statement":"SELECT 1"}' })
    expect(n1IssueForSpan(s, [])).toBeNull()
    // Wrong kind shouldn't match either.
    const otherKind = issue({ id: 'i2', kind: 'slow', parent_span_id: 'p', example_span_id: 'db1' })
    expect(n1IssueForSpan(s, [otherKind])).toBeNull()
  })

  it('matches when the span is the issue example_span_id', () => {
    const s = span({ span_id: 'db1', parent_span_id: 'p',
      attributes: '{"db.statement":"SELECT 1"}' })
    const i = issue({ id: 'i1', example_span_id: 'db1' })
    expect(n1IssueForSpan(s, [i])?.id).toBe('i1')
  })

  it('matches a sibling under the same parent (clustered by parent + fingerprint)', () => {
    const sibling = span({ span_id: 'db2', parent_span_id: 'p',
      attributes: '{"db.statement":"SELECT 1"}' })
    const i = issue({ id: 'i1', parent_span_id: 'p', example_span_id: 'db1' })
    expect(n1IssueForSpan(sibling, [i])?.id).toBe('i1')
  })

  it('does not match a sibling with a different parent', () => {
    const otherParent = span({ span_id: 'db3', parent_span_id: 'other',
      attributes: '{"db.statement":"SELECT 1"}' })
    const i = issue({ id: 'i1', parent_span_id: 'p', example_span_id: 'db1' })
    expect(n1IssueForSpan(otherParent, [i])).toBeNull()
  })
})
