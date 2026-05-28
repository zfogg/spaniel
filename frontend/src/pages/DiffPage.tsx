import { useEffect, useState, useCallback, useMemo } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { api, type Span, type Session } from '@/lib/api'
import { fmtNs, svcColor, flatten } from '@/lib/span-utils'
import { diffStatusFor, layoutSpan, columnWindow, sharedWindowNs, type DiffStatus } from '@/lib/diff-layout'
import { updateDiffHistoryDeltas } from '@/lib/diff-history'
import EmptyState from '@/components/EmptyState'

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
    <div className="min-w-24">
      <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3">
        {label}
      </div>
      <div className="flex items-baseline gap-1.5 mt-0.5">
        <span className="font-serif text-lg font-semibold text-ink">
          {fmtValue(after)}{unit && !raw ? unit : ''}
        </span>
        {delta !== 0 && (
          <span
            className="font-mono text-[10px] font-semibold"
            style={{ color: better ? '#3e6a3e' : 'var(--danger-ink)' }}
          >
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
    <span
      className="inline-flex items-center px-[7px] py-px rounded-[10px] font-mono text-[9.5px] font-semibold whitespace-nowrap leading-[1.4] border"
      style={{ background: t.bg, color: t.fg, borderColor: t.bd }}
    >
      {children}
    </span>
  )
}

// ── WaterfallRow ──────────────────────────────────────────────────────────────

function WaterfallRow({
  span,
  depth,
  status,
  leftPct,
  widthPct,
}: {
  span: Span
  depth: number
  status: DiffStatus
  leftPct: number
  widthPct: number
}) {
  const color = svcColor(span.service_name)

  const rowBg: Record<DiffStatus, string> = {
    removed:   'color-mix(in oklch, var(--danger) 10%, var(--surface))',
    added:     'color-mix(in oklch, var(--ok)     10%, var(--surface))',
    changed:   'color-mix(in oklch, var(--warn)   10%, var(--surface))',
    unchanged: 'transparent',
  }

  const namePos = Math.min(leftPct + widthPct + 0.6, 70)

  return (
    <div
      data-status={status}
      className="grid items-center px-2 py-0.5 border-b border-line2"
      style={{ gridTemplateColumns: '94px minmax(0,1fr) 50px', gap: 8, background: rowBg[status] }}
    >
      {/* service chip */}
      <div className="flex items-center gap-[5px] min-w-0" style={{ paddingLeft: depth * 8 }}>
        <span className="w-[6px] h-[6px] rounded-full opacity-85 flex-none" style={{ background: color.fg }} />
        <span
          className="font-mono text-[9.5px] font-semibold tracking-[-0.01em] overflow-hidden text-ellipsis whitespace-nowrap"
          style={{ color: color.fg }}
        >
          {span.service_name.replace(/-service$/, '')}
        </span>
      </div>

      {/* proportional bar + name label */}
      <div className="relative h-[14px]" style={{ marginLeft: depth * 8 }}>
        <div
          className="absolute rounded-[2px]"
          style={{
            top: 3, height: 8,
            left: `${leftPct}%`, width: `${widthPct}%`,
            background: color.bg,
            boxShadow: `inset 2px 0 0 ${color.fg}`,
            opacity: status === 'removed' ? 0.45 : 1,
          }}
        />
        <span
          className={`absolute top-0 font-mono text-[9.5px] text-ink2 whitespace-nowrap overflow-hidden text-ellipsis${status === 'removed' ? ' line-through opacity-60' : ''}`}
          style={{ left: `${namePos}%`, maxWidth: '100%' }}
        >
          {span.name}
        </span>
      </div>

      {/* duration */}
      <div className="font-mono text-[9.5px] text-ink2 text-right whitespace-nowrap">
        {fmtNs(span.duration_ns)}
      </div>
    </div>
  )
}

// ── WaterfallColumn ───────────────────────────────────────────────────────────

function WaterfallColumn({
  side,
  label,
  totalDurNs,
  spans,
  diffSpans,
  startNs,
  windowNs,
}: {
  side: 'baseline' | 'compare'
  label: string
  totalDurNs: number
  spans: Span[]
  diffSpans: DiffSpan[]
  startNs: number
  windowNs: number
}) {
  const isBaseline = side === 'baseline'
  const flatSpans = useMemo(() => flatten(spans), [spans])

  return (
    <div className="flex-1 flex flex-col overflow-hidden bg-surface">
      {/* column header */}
      <div
        className="px-3.5 py-2.5 border-b border-line flex items-center gap-2.5 shrink-0"
        style={{
          background: isBaseline
            ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
            : 'color-mix(in oklch, var(--ok) 18%, var(--surface))',
        }}
      >
        <Badge tone={isBaseline ? 'accent' : 'ok'}>
          {isBaseline ? 'baseline' : 'compare'}
        </Badge>
        <span className="font-mono text-[11px] text-ink flex-1">{label}</span>
        <span className="font-serif text-lg font-semibold text-ink">{fmtNs(totalDurNs)}</span>
      </div>

      {/* waterfall rows */}
      <div className="flex-1 overflow-y-auto">
        {flatSpans.length === 0 ? (
          <div className="px-4 py-6 text-center font-mono text-[11px] text-ink3">no spans</div>
        ) : (
          flatSpans.map(({ span, depth }) => {
            const status = diffStatusFor(span, diffSpans)
            const { leftPct, widthPct } = layoutSpan(span, startNs, windowNs)
            return (
              <WaterfallRow
                key={span.span_id}
                span={span}
                depth={depth}
                status={status}
                leftPct={leftPct}
                widthPct={widthPct}
              />
            )
          })
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

  const selectClass = "font-mono text-[11px] bg-surface2 text-ink border border-line rounded-md h-7 px-2 outline-none cursor-pointer min-w-[160px]"

  const baselineSess = sessions.find(s => s.id === baselineId)
  const compareSess = sessions.find(s => s.id === compareId)

  // Shared time scale so both columns' bars are proportionally comparable.
  const windowNs = useMemo(
    () => diff ? sharedWindowNs(diff.baseline_spans, diff.compare_spans) : 0,
    [diff],
  )
  const baseStartNs = useMemo(
    () => diff?.baseline_spans.length ? columnWindow(diff.baseline_spans).startNs : 0,
    [diff],
  )
  const cmpStartNs = useMemo(
    () => diff?.compare_spans.length ? columnWindow(diff.compare_spans).startNs : 0,
    [diff],
  )

  return (
    <div className="flex flex-col h-full overflow-hidden">

      {/* 1. Top breadcrumb bar */}
      <div className="h-9 px-3 border-b border-line bg-surface flex items-center gap-2.5 shrink-0">
        <button
          type="button"
          onClick={() => navigate(-1)}
          className="font-mono text-xs bg-transparent border-none cursor-pointer text-ink2 px-1"
        >
          ← back
        </button>

        <div className="w-px h-[18px] bg-line shrink-0" />

        <span className="font-mono text-[10px] text-ink3">
          baseline
        </span>
        <select
          className={selectClass}
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
          className="font-mono text-sm bg-transparent border border-line rounded-md cursor-pointer text-ink2 w-7 h-7 inline-flex items-center justify-center shrink-0"
        >
          ⇄
        </button>

        <span className="font-mono text-[10px] text-ink3">
          compare
        </span>
        <select
          className={selectClass}
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
        <div className="px-[18px] py-3.5 border-b border-line bg-surface flex items-center gap-[18px] shrink-0">
          <div className="flex-1">
            <div className="font-serif text-lg font-semibold text-ink">
              {diff.baseline.label}
            </div>
            <div className="font-mono text-[10px] text-ink3">
              baseline @{' '}
              <span className="text-ink2">{diff.baseline.label}</span>
              {' '}↔ compare @{' '}
              <span className="text-ink2">{diff.compare.label}</span>
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
      <div className="flex-1 flex min-h-0 overflow-hidden">
        {loading ? (
          <div className="flex-1 flex items-center justify-center font-mono text-[13px] text-ink3">
            computing diff…
          </div>
        ) : !baselineId || !compareId ? (
          <EmptyState
            title="No diff yet"
            hint="Pick a baseline and compare session on the Sessions page."
            glyph={
              <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                <rect x="4" y="8" width="10" height="16" rx="2" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                <rect x="18" y="8" width="10" height="16" rx="2" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                <line x1="14" y1="16" x2="18" y2="16" stroke="currentColor" strokeWidth="1.5" opacity="0.4" strokeDasharray="2 2" />
              </svg>
            }
          />
        ) : !diff ? null : diff.spans.length === 0 ? (
          <div className="flex-1 flex items-center justify-center font-mono text-[13px] text-ink3">
            no shared operations found between these sessions
          </div>
        ) : (
          <>
            <WaterfallColumn
              side="baseline"
              label={diff.baseline.label}
              totalDurNs={diff.baseline.total_duration_ns}
              spans={diff.baseline_spans}
              diffSpans={diff.spans}
              startNs={baseStartNs}
              windowNs={windowNs}
            />
            <div className="w-px bg-line shrink-0" />
            <WaterfallColumn
              side="compare"
              label={diff.compare.label}
              totalDurNs={diff.compare.total_duration_ns}
              spans={diff.compare_spans}
              diffSpans={diff.spans}
              startNs={cmpStartNs}
              windowNs={windowNs}
            />
          </>
        )}
      </div>

      {/* 4. Footer insights strip */}
      {diff && (
        <div className="px-4 py-2.5 border-t border-line bg-surface2 flex items-center gap-[18px] shrink-0 font-mono text-[11px] text-ink2">
          {diff.summary.spans_removed > 0 && (
            <span>
              <strong style={{ color: '#3e6a3e' }}>−{diff.summary.spans_removed}</strong> spans removed
            </span>
          )}
          {diff.summary.spans_added > 0 && (
            <span>
              <strong className="text-danger-ink">+{diff.summary.spans_added}</strong> spans added
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
          <div className="flex-1" />
          <span className="px-2 py-1 border border-line rounded-[5px] bg-surface">
            spaniel diff --baseline {baselineSess?.label ?? diff.baseline.label}
          </span>
        </div>
      )}
    </div>
  )
}
