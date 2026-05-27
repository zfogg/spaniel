import { useEffect, useState, useCallback } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { api, Span, Session } from '@/lib/api'
import { fmtNs, svcColor } from '@/lib/span-utils'
import { updateDiffHistoryDeltas } from '@/lib/diff-history'

// ── types ─────────────────────────────────────────────────────────────────────

interface DiffSpan {
  name: string
  service_name: string
  status: string
  baseline_duration_ns: number
  compare_duration_ns: number
  delta_pct: number
  depth: number
}

interface DiffSessionInfo {
  session_id: string
  label: string
  total_duration_ns: number
  span_count: number
  db_calls: number
}

interface DiffSummary {
  duration_delta_ns: number
  duration_delta_pct: number
  spans_added: number
  spans_removed: number
  db_call_delta: number
}

interface DiffResult {
  baseline: DiffSessionInfo
  compare: DiffSessionInfo
  summary: DiffSummary
  spans: DiffSpan[]
  baseline_spans: Span[]
  compare_spans: Span[]
}

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtMs(ns: number): string {
  return fmtNs(ns)
}

// ── DiffStat ──────────────────────────────────────────────────────────────────

function DiffStat({
  label,
  before,
  after,
  unit,
  raw,
}: {
  label: string
  before: number
  after: number
  unit: string
  raw?: boolean
}) {
  const delta = after - before
  const better = delta < 0

  const fmtValue = (v: number) => {
    if (raw) return String(v)
    return fmtMs(v)
  }

  const fmtDelta = (d: number) => {
    const abs = Math.abs(d)
    const sign = d > 0 ? '+' : d < 0 ? '−' : ''
    if (raw) return `${sign}${abs}`
    return `${sign}${fmtMs(abs)}`
  }

  return (
    <div style={{ minWidth: 96 }}>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 9, textTransform: 'uppercase',
        letterSpacing: '0.14em', color: 'var(--ink3)',
      }}>
        {label}
      </div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginTop: 2 }}>
        <span style={{
          fontFamily: 'var(--font-serif)', fontSize: 18, fontWeight: 600, color: 'var(--ink)',
        }}>
          {fmtValue(after)}{unit && !raw ? unit : ''}
        </span>
        {delta !== 0 && (
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600,
            color: better ? '#3e6a3e' : 'var(--danger-ink)',
          }}>
            {fmtDelta(delta)}
          </span>
        )}
      </div>
    </div>
  )
}

// ── Badge ─────────────────────────────────────────────────────────────────────

function Badge({ tone, children }: { tone: 'accent' | 'ok'; children: React.ReactNode }) {
  const tones = {
    accent: {
      bg: 'color-mix(in oklch, var(--accent) 18%, var(--surface))',
      fg: 'var(--accent-ink)',
      bd: 'color-mix(in oklch, var(--accent) 40%, transparent)',
    },
    ok: {
      bg: 'color-mix(in oklch, var(--ok) 22%, var(--surface))',
      fg: 'var(--ok-ink)',
      bd: 'color-mix(in oklch, var(--ok) 40%, transparent)',
    },
  }
  const t = tones[tone]
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center',
      padding: '1px 7px', borderRadius: 10,
      fontFamily: 'var(--font-mono)', fontSize: 9.5, fontWeight: 600,
      background: t.bg, color: t.fg, border: `1px solid ${t.bd}`,
      whiteSpace: 'nowrap', lineHeight: 1.4,
    }}>
      {children}
    </span>
  )
}

// ── SpanRow ───────────────────────────────────────────────────────────────────

function SpanRow({
  ds,
  side,
}: {
  ds: DiffSpan
  side: 'baseline' | 'compare'
}) {
  const dur = side === 'baseline' ? ds.baseline_duration_ns : ds.compare_duration_ns

  let rowBg = 'transparent'
  if (side === 'baseline') {
    if (ds.status === 'removed') {
      rowBg = 'color-mix(in oklch, var(--danger) 10%, var(--surface))'
    }
  } else {
    if (ds.status === 'added') {
      rowBg = 'color-mix(in oklch, var(--ok) 10%, var(--surface))'
    } else if (ds.status === 'changed' && ds.delta_pct > 5) {
      rowBg = 'color-mix(in oklch, var(--danger) 10%, var(--surface))'
    } else if (ds.status === 'changed' && ds.delta_pct < -5) {
      rowBg = 'color-mix(in oklch, var(--ok) 10%, var(--surface))'
    }
  }

  const color = svcColor(ds.service_name)

  const showDelta = side === 'compare' && ds.status === 'changed' && Math.abs(ds.delta_pct) > 0
  const deltaPositive = ds.delta_pct > 0

  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: '94px minmax(0,1fr) auto',
      gap: 8, alignItems: 'center',
      padding: '2px 8px',
      borderBottom: '1px solid var(--line2)',
      background: rowBg,
    }}>
      {/* service */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 5,
        paddingLeft: ds.depth * 8,
        minWidth: 0,
      }}>
        <span style={{
          width: 5, height: 5, borderRadius: 5,
          background: color.fg, opacity: 0.85, flex: '0 0 auto',
        }} />
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 9.5, color: color.fg,
          fontWeight: 600, letterSpacing: '-0.01em',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}>
          {ds.service_name.replace(/-service$/, '')}
        </span>
      </div>

      {/* span name */}
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 9.5, color: 'var(--ink2)',
        overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        paddingLeft: ds.depth * 8,
      }}>
        {ds.name}
      </span>

      {/* duration + optional delta badge */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 5, justifyContent: 'flex-end', flexShrink: 0 }}>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 9.5, color: 'var(--ink2)',
          textAlign: 'right', whiteSpace: 'nowrap',
        }}>
          {fmtNs(dur)}
        </span>
        {showDelta && (
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
            color: deltaPositive ? 'var(--danger-ink)' : '#3e6a3e',
            whiteSpace: 'nowrap',
          }}>
            {deltaPositive ? '+' : '−'}{Math.abs(Math.round(ds.delta_pct))}%
          </span>
        )}
      </div>
    </div>
  )
}

// ── SpanColumn ────────────────────────────────────────────────────────────────

function SpanColumn({
  side,
  label,
  totalDurNs,
  spans,
}: {
  side: 'baseline' | 'compare'
  label: string
  totalDurNs: number
  spans: DiffSpan[]
}) {
  const isBaseline = side === 'baseline'
  const visibleSpans = spans.filter(ds =>
    isBaseline ? ds.baseline_duration_ns > 0 : ds.compare_duration_ns > 0
  )

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: 'var(--surface)' }}>
      {/* column header */}
      <div style={{
        padding: '10px 14px', borderBottom: '1px solid var(--line)',
        background: isBaseline
          ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
          : 'color-mix(in oklch, var(--ok) 18%, var(--surface))',
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
      }}>
        <Badge tone={isBaseline ? 'accent' : 'ok'}>
          {isBaseline ? 'baseline' : 'compare'}
        </Badge>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink)', flex: 1 }}>
          {label}
        </span>
        <span style={{ fontFamily: 'var(--font-serif)', fontSize: 18, fontWeight: 600, color: 'var(--ink)' }}>
          {fmtNs(totalDurNs)}
        </span>
      </div>

      {/* span rows */}
      <div style={{ flex: 1, overflowY: 'auto' }}>
        {visibleSpans.length === 0 ? (
          <div style={{
            padding: '24px 16px', textAlign: 'center',
            fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink3)',
          }}>
            no spans
          </div>
        ) : (
          visibleSpans.map((ds, i) => (
            <SpanRow key={`${ds.name}-${ds.service_name}-${i}`} ds={ds} side={side} />
          ))
        )}
      </div>
    </div>
  )
}

// ── DiffPage ──────────────────────────────────────────────────────────────────

export default function DiffPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()

  const [sessions, setSessions] = useState<Session[]>([])
  const [baselineId, setBaselineId] = useState(searchParams.get('baseline') ?? '')
  const [compareId, setCompareId] = useState(searchParams.get('compare') ?? '')
  const [diff, setDiff] = useState<DiffResult | null>(null)
  const [loading, setLoading] = useState(false)

  // load sessions list for selectors
  useEffect(() => {
    api.sessions.list().then(res => setSessions(res.data ?? []))
  }, [])

  // fetch diff when both ids present
  const fetchDiff = useCallback(async (bId: string, cId: string) => {
    if (!bId || !cId) { setDiff(null); return }
    setLoading(true)
    try {
      const res = await fetch(`/api/diff?baseline=${bId}&compare=${cId}`)
      const json = await res.json()
      const result = json.data
      setDiff(result)
      if (result?.summary) {
        updateDiffHistoryDeltas(
          bId, cId,
          Math.round(result.summary.duration_delta_ns / 1_000_000),
          (result.summary.spans_added ?? 0) - (result.summary.spans_removed ?? 0),
        )
      }
    } finally {
      setLoading(false)
    }
  }, [])

  // sync URL params → state on mount
  useEffect(() => {
    const b = searchParams.get('baseline') ?? ''
    const c = searchParams.get('compare') ?? ''
    if (b !== baselineId) setBaselineId(b)
    if (c !== compareId) setCompareId(c)
    if (b && c) fetchDiff(b, c)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function updateIds(newBaseline: string, newCompare: string) {
    setBaselineId(newBaseline)
    setCompareId(newCompare)
    const params: Record<string, string> = {}
    if (newBaseline) params.baseline = newBaseline
    if (newCompare) params.compare = newCompare
    setSearchParams(params)
    fetchDiff(newBaseline, newCompare)
  }

  function handleSwap() {
    updateIds(compareId, baselineId)
  }

  // ── select style ──
  const selectStyle: React.CSSProperties = {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    background: 'var(--surface2)',
    color: 'var(--ink)',
    border: '1px solid var(--line)',
    borderRadius: 6,
    height: 28,
    padding: '0 8px',
    outline: 'none',
    cursor: 'pointer',
    minWidth: 160,
  }

  const baselineSess = sessions.find(s => s.id === baselineId)
  const compareSess = sessions.find(s => s.id === compareId)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>

      {/* 1. Top breadcrumb bar */}
      <div style={{
        height: 36, padding: '0 12px',
        borderBottom: '1px solid var(--line)',
        background: 'var(--surface)',
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
      }}>
        <button
          type="button"
          onClick={() => navigate(-1)}
          style={{
            fontFamily: 'var(--font-mono)', fontSize: 12,
            background: 'transparent', border: 'none', cursor: 'pointer',
            color: 'var(--ink2)', padding: '0 4px',
          }}
        >
          ← back
        </button>

        <div style={{ width: 1, height: 18, background: 'var(--line)', flexShrink: 0 }} />

        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)' }}>
          baseline
        </span>
        <select
          style={selectStyle}
          value={baselineId}
          onChange={e => updateIds(e.target.value, compareId)}
        >
          <option value="">— select baseline —</option>
          {sessions.map(s => (
            <option key={s.id} value={s.id}>{s.label || s.id.slice(0, 8)}</option>
          ))}
        </select>

        <button
          type="button"
          onClick={handleSwap}
          title="Swap baseline and compare"
          style={{
            fontFamily: 'var(--font-mono)', fontSize: 14,
            background: 'transparent', border: '1px solid var(--line)', borderRadius: 6,
            cursor: 'pointer', color: 'var(--ink2)',
            width: 28, height: 28, display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            flexShrink: 0,
          }}
        >
          ⇄
        </button>

        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)' }}>
          compare
        </span>
        <select
          style={selectStyle}
          value={compareId}
          onChange={e => updateIds(baselineId, e.target.value)}
        >
          <option value="">— select compare —</option>
          {sessions.map(s => (
            <option key={s.id} value={s.id}>{s.label || s.id.slice(0, 8)}</option>
          ))}
        </select>
      </div>

      {/* 2. Summary strip */}
      {diff && (
        <div style={{
          padding: '14px 18px', borderBottom: '1px solid var(--line)',
          background: 'var(--surface)',
          display: 'flex', alignItems: 'center', gap: 18, flexShrink: 0,
        }}>
          <div style={{ flex: 1 }}>
            <div style={{
              fontFamily: 'var(--font-serif)', fontSize: 18, fontWeight: 600, color: 'var(--ink)',
            }}>
              {diff.baseline.label}
            </div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)' }}>
              baseline @{' '}
              <span style={{ color: 'var(--ink2)' }}>{diff.baseline.label}</span>
              {' '}↔ compare @{' '}
              <span style={{ color: 'var(--ink2)' }}>{diff.compare.label}</span>
            </div>
          </div>

          <DiffStat
            label="total"
            before={diff.baseline.total_duration_ns}
            after={diff.compare.total_duration_ns}
            unit="ms"
          />
          <DiffStat
            label="spans"
            before={diff.baseline.span_count}
            after={diff.compare.span_count}
            unit=""
            raw
          />
          <DiffStat
            label="db calls"
            before={diff.baseline.db_calls}
            after={diff.compare.db_calls}
            unit=""
            raw
          />
        </div>
      )}

      {/* 3. Main body */}
      <div style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
        {loading ? (
          <div style={{
            flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--ink3)',
          }}>
            computing diff…
          </div>
        ) : !baselineId || !compareId ? (
          <div style={{
            flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--ink3)',
          }}>
            select two sessions to compare
          </div>
        ) : !diff ? null : diff.spans.length === 0 ? (
          <div style={{
            flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--ink3)',
          }}>
            no shared operations found between these sessions
          </div>
        ) : (
          <>
            <SpanColumn
              side="baseline"
              label={diff.baseline.label}
              totalDurNs={diff.baseline.total_duration_ns}
              spans={diff.spans}
            />
            <div style={{ width: 1, background: 'var(--line)', flexShrink: 0 }} />
            <SpanColumn
              side="compare"
              label={diff.compare.label}
              totalDurNs={diff.compare.total_duration_ns}
              spans={diff.spans}
            />
          </>
        )}
      </div>

      {/* 4. Footer insights strip */}
      {diff && (
        <div style={{
          padding: '10px 16px', borderTop: '1px solid var(--line)',
          background: 'var(--surface2)',
          display: 'flex', alignItems: 'center', gap: 18, flexShrink: 0,
          fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink2)',
        }}>
          {diff.summary.spans_removed > 0 && (
            <span>
              <strong style={{ color: '#3e6a3e' }}>−{diff.summary.spans_removed}</strong> spans removed
            </span>
          )}
          {diff.summary.spans_added > 0 && (
            <span>
              <strong style={{ color: 'var(--danger-ink)' }}>+{diff.summary.spans_added}</strong> spans added
            </span>
          )}
          {diff.summary.db_call_delta !== 0 && (
            <span>
              db calls:{' '}
              <strong style={{ color: diff.summary.db_call_delta < 0 ? '#3e6a3e' : 'var(--danger-ink)' }}>
                {diff.summary.db_call_delta > 0 ? '+' : ''}{diff.summary.db_call_delta}
              </strong>
            </span>
          )}
          <div style={{ flex: 1 }} />
          <span style={{
            padding: '4px 8px', border: '1px solid var(--line)',
            borderRadius: 5, background: 'var(--surface)',
          }}>
            spaniel diff --baseline {baselineSess?.label ?? diff.baseline.label}
          </span>
        </div>
      )}
    </div>
  )
}
