// Pure helpers extracted from TraceWaterfall.tsx so they're testable in
// isolation. Each function is a function of its inputs only — no React, no
// DOM, no module state.

import type { Span, TraceIssue } from '@/lib/api'

// ── N+1 issue lookup for a single span (inspector callout) ─────────────────
import type { FlatSpan } from '@/lib/span-utils'

const ZERO_ID = '0000000000000000'

// ── timeline layout ─────────────────────────────────────────────────────────

export interface SpanLayout {
  leftPct: number
  widthPct: number
}

/** Position a span on the waterfall track as left/width percentages of the
 *  total trace duration. Mirrors the inline math in SpanRow. Width clamps
 *  to a minimum so zero-duration spans remain visible. */
export function computeLayout(span: Span, traceStartNs: number, traceDurNs: number): SpanLayout {
  if (traceDurNs <= 0) return { leftPct: 0, widthPct: 0.6 }
  const leftPct = ((span.start_ns - traceStartNs) / traceDurNs) * 100
  const widthPct = Math.max(0.6, (span.duration_ns / traceDurNs) * 100)
  return { leftPct, widthPct }
}

// ── critical path ───────────────────────────────────────────────────────────

function isRoot(s: Span): boolean {
  return !s.parent_span_id || s.parent_span_id === ZERO_ID
}

/** Span IDs on the trace's critical path: starting at the root, follow the
 *  longest-duration child at each step. Ties broken by latest-ending span.
 *  Returns an empty set when no root exists. */
export function criticalPath(spans: Span[]): Set<string> {
  if (spans.length === 0) return new Set()
  const childrenOf = new Map<string, Span[]>()
  for (const s of spans) {
    if (isRoot(s)) continue
    const kids = childrenOf.get(s.parent_span_id) ?? []
    kids.push(s)
    childrenOf.set(s.parent_span_id, kids)
  }
  const root = spans.find(isRoot)
  if (!root) return new Set()
  const path = new Set<string>()
  let cur: Span | undefined = root
  while (cur) {
    path.add(cur.span_id)
    const kids: Span[] = childrenOf.get(cur.span_id) ?? []
    if (kids.length === 0) break
    let best: Span = kids[0]
    for (const k of kids) {
      if (k.duration_ns > best.duration_ns) best = k
      else if (k.duration_ns === best.duration_ns && k.end_ns > best.end_ns) best = k
    }
    cur = best
  }
  return path
}

// ── N+1 detection ──────────────────────────────────────────────────────────

const N1_THRESHOLD = 10

function attrs(span: Span): Record<string, unknown> {
  try { return JSON.parse(span.attributes ?? '{}') } catch { return {} }
}

/** Span IDs that should be flagged with the N+1 badge. Combines server-side
 *  issues (when present, by example_span_id) with a client-side fallback that
 *  groups spans sharing the same raw `db.statement` or `db.query.text` and
 *  flags any group with 10+ occurrences. */
export function detectN1SpanIds(flatSpans: FlatSpan[], issues: TraceIssue[]): Set<string> {
  const set = new Set<string>()
  for (const issue of issues) {
    if (issue.kind === 'n_plus_one' && issue.example_span_id) {
      set.add(issue.example_span_id)
    }
  }
  // Client-side fallback: group by db.statement or db.query.text.
  const counts = new Map<string, string[]>()
  for (const flat of flatSpans) {
    const a = attrs(flat.span)
    const stmt = (typeof a['db.statement'] === 'string' && a['db.statement']) ||
                 (typeof a['db.query.text'] === 'string' && a['db.query.text'])
    if (!stmt) continue
    const ids = counts.get(stmt) ?? []
    ids.push(flat.span.span_id)
    counts.set(stmt, ids)
  }
  for (const ids of counts.values()) {
    if (ids.length >= N1_THRESHOLD) {
      for (const id of ids) set.add(id)
    }
  }
  return set
}

export interface N1BannerEntry {
  fingerprint: string
  count: number
  wastedNs: number
}

/** Summary entries for the N+1 banner. Prefers server issues when present;
 *  otherwise derives from client-side `db.statement` grouping. Sorted by
 *  wasted time descending. */
/** Returns the n_plus_one TraceIssue that implicates this span, or null.
 *  A span is implicated when it has a `db.statement` attribute and either
 *  it IS the issue's example span or it shares the issue's parent span
 *  (the server clusters by parent + statement fingerprint). */
export function n1IssueForSpan(span: Span | null, issues: TraceIssue[]): TraceIssue | null {
  if (!span) return null
  let a: Record<string, unknown> = {}
  try { a = JSON.parse(span.attributes || '{}') } catch { /* empty */ }
  const hasStmt = typeof a['db.statement'] === 'string' || typeof a['db.query.text'] === 'string'
  if (!hasStmt) return null
  for (const issue of issues) {
    if (issue.kind !== 'n_plus_one') continue
    if (issue.example_span_id && issue.example_span_id === span.span_id) return issue
    if (issue.parent_span_id && span.parent_span_id && issue.parent_span_id === span.parent_span_id) return issue
  }
  return null
}

export function n1BannerEntries(flatSpans: FlatSpan[], issues: TraceIssue[]): N1BannerEntry[] {
  const server = issues
    .filter(i => i.kind === 'n_plus_one')
    .map(i => ({ fingerprint: i.fingerprint, count: i.count, wastedNs: i.wasted_ns }))
  if (server.length > 0) return server.sort((a, b) => b.wastedNs - a.wastedNs)

  const groups = new Map<string, { ids: string[]; totalNs: number }>()
  for (const flat of flatSpans) {
    const a = attrs(flat.span)
    const stmt = (typeof a['db.statement'] === 'string' && a['db.statement']) ||
                 (typeof a['db.query.text'] === 'string' && a['db.query.text'])
    if (!stmt) continue
    const g = groups.get(stmt) ?? { ids: [], totalNs: 0 }
    g.ids.push(flat.span.span_id)
    g.totalNs += flat.span.duration_ns
    groups.set(stmt, g)
  }
  const out: N1BannerEntry[] = []
  for (const [fp, { ids, totalNs }] of groups) {
    if (ids.length >= N1_THRESHOLD) out.push({ fingerprint: fp, count: ids.length, wastedNs: totalNs })
  }
  return out.sort((a, b) => b.wastedNs - a.wastedNs)
}
