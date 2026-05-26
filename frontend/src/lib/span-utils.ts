import type { Span, LintWarning } from '@/lib/api'

// ── palette & svc color ───────────────────────────────────────────────────────

export const SPAN_PALETTE = [
  '#7aa3c4', '#88b29a', '#d6b46a', '#a08cc8',
  '#6a98b8', '#8ab8a0', '#c8a870', '#7898c0',
  '#d69882', '#98a8c0',
]

export const SPAN_ACCENT = '#7aa3c4'

export function svcColor(name: string): { fg: string; bg: string } {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (Math.imul(31, h) + name.charCodeAt(i)) | 0
  const fg = SPAN_PALETTE[Math.abs(h) % SPAN_PALETTE.length]
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
