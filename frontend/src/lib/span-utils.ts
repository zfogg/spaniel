import type { Span, LintWarning } from '@/lib/api'

// ── palette & svc color ───────────────────────────────────────────────────────
// Re-exported from the canonical Drift palette in `tokens.ts`. Service color
// is hash-based so a name maps to the same slot across sessions/screens.

import { SPAN_PALETTE as DRIFT_PALETTE, svcHex } from '@/lib/tokens'

export const SPAN_PALETTE = DRIFT_PALETTE
export const SPAN_ACCENT = DRIFT_PALETTE[0]

export function svcColor(name: string): { fg: string; bg: string } {
  const fg = svcHex(name)
  return { fg, bg: fg + '28' }
}

// ── tree helpers ──────────────────────────────────────────────────────────────

const ZERO_ID = '0000000000000000'

export interface FlatSpan {
  span: Span
  depth: number
  orphan: boolean
}

export function flatten(spans: Span[]): FlatSpan[] {
  const byId = new Map(spans.map(s => [s.span_id, s]))
  const children = new Map<string, Span[]>()
  for (const s of spans) {
    const pid = s.parent_span_id && s.parent_span_id !== ZERO_ID && byId.has(s.parent_span_id)
      ? s.parent_span_id : ''
    if (!children.has(pid)) children.set(pid, [])
    children.get(pid)!.push(s)
  }
  const orphanIds = new Set(
    spans
      .filter(s => s.parent_span_id && s.parent_span_id !== ZERO_ID && !byId.has(s.parent_span_id))
      .map(s => s.span_id),
  )
  const result: FlatSpan[] = []
  function walk(pid: string, depth: number) {
    for (const s of children.get(pid) ?? []) {
      result.push({ span: s, depth, orphan: orphanIds.has(s.span_id) })
      walk(s.span_id, depth + 1)
    }
  }
  walk('', 0)
  return result
}

// ── formatting ────────────────────────────────────────────────────────────────

export function fmtNs(ns: number): string {
  if (ns < 1_000) return `${ns}ns`
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

export const KIND_LABELS = ['unspecified', 'internal', 'server', 'client', 'producer', 'consumer']

// ── tag computation ───────────────────────────────────────────────────────────

export function buildTagMap(warnings: LintWarning[]): Map<string, string> {
  const m = new Map<string, string>()
  for (const w of warnings) {
    if (!m.has(w.span_id)) {
      m.set(w.span_id, w.rule_id.includes('N+1') ? 'n+1' : 'lint')
    }
  }
  return m
}

// hasLinks: true when the span carries at least one OTel span link. The
// waterfall uses this to render the chain-icon badge.
export function hasLinks(span: { links?: { linked_trace_id?: string }[] } | null | undefined): boolean {
  if (!span || !span.links) return false
  return span.links.length > 0
}

// shortTraceId returns the standard "9ef3…d19eb" abbreviation used in
// the link rows and several inspector affordances.
export function shortTraceId(id: string): string {
  if (!id || id.length < 12) return id
  return `${id.slice(0, 4)}…${id.slice(-5)}`
}

// ── HTTP display name ─────────────────────────────────────────────────────────

/** Build a human-friendly display name for HTTP spans.
 *  Tries url.full then http.url (both carry the full URL), falling back to
 *  url.path / http.target for the path alone.  When the name contains
 *  "unknown" and we can't build something better, returns just the method. */
export function httpDisplayName(span: { name: string; attributes?: string | null }): string {
  try {
    const a = JSON.parse(span.attributes ?? '{}')
    // Extract HTTP method: prefer attributes, then parse from name like "HTTP GET"
    let method = a['http.request.method'] || a['http.method'] || ''
    if (!method) {
      const m = span.name.match(/^(?:HTTP)\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|TRACE)$/i)
      if (m) method = m[1].toUpperCase()
    }
    // Try full URLs: url.full first, then http.url
    for (const raw of [a['url.full'], a['http.url']]) {
      if (!raw) continue
      try {
        const u = new URL(raw)
        return method ? `${method} ${u.host}${u.pathname}` : `${u.host}${u.pathname}`
      } catch {
        // Not a full URL — use as bare path (e.g. "/api/foo")
        if (raw.startsWith('/')) return method ? `${method} ${raw}` : raw
      }
    }
    // Fallback: url.path or http.target with method
    const path = a['url.path'] || a['http.target'] || ''
    if (method && path) return `${method} ${path}`
    if (path) return path
  } catch { /* empty */ }
  // Avoid returning raw "unknown" — use just the method if we have one
  if (span.name.includes('unknown')) {
    const m = span.name.match(/^(?:HTTP)\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|TRACE)/i)
    if (m) return m[1].toUpperCase()
  }
  return span.name
}
