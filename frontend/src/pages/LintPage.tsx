import { useEffect, useState } from 'react'
import { api, LintWarning, TraceIssue } from '@/lib/api'
import { useWS } from '@/lib/ws'
import EmptyState from '@/components/EmptyState'

// ── Severity helpers ──────────────────────────────────────────────────────────

function sevDot(sev: string) {
  if (sev === 'error') return 'var(--danger)'
  if (sev === 'warning') return 'var(--warn)'
  return 'var(--ink3)'
}

function sevBadgeClass(sev: string) {
  if (sev === 'error') return 'text-danger-ink bg-danger-bg'
  if (sev === 'warning') return 'text-warn-ink bg-warn-bg'
  return 'text-ink2 bg-surface2'
}

// ── SummaryStat ───────────────────────────────────────────────────────────────

interface SummaryStatProps {
  tone: 'danger' | 'warn' | 'ok' | 'neutral'
  big: string | number
  label: string
  sub: string
}

function SummaryStat({ tone, big, label, sub }: SummaryStatProps) {
  const dotClass = {
    danger:  'bg-danger',
    warn:    'bg-warn',
    ok:      'bg-ok',
    neutral: 'bg-ink3',
  }[tone]
  return (
    <div className="flex-1 px-3 py-2.5 border border-line rounded-lg bg-surface2">
      <div className="flex items-baseline gap-2">
        <span className={`w-2 h-2 rounded-full self-center shrink-0 ${dotClass}`} />
        <span className="font-mono text-xl font-bold text-ink leading-none">{big}</span>
      </div>
      <div className="font-sans text-[11px] text-ink2 mt-1">{label}</div>
      <div className="font-mono text-[10px] text-ink3 mt-0.5">{sub}</div>
    </div>
  )
}

// ── LintRow ───────────────────────────────────────────────────────────────────

function LintRow({ w, last }: { w: LintWarning; last: boolean }) {
  const dot = sevDot(w.severity)
  const badgeClass = sevBadgeClass(w.severity)

  return (
    <div
      className={`px-[18px] py-3 flex gap-3 items-start ${last ? '' : 'border-b border-line2'}`}
    >
      {/* severity dot */}
      <span
        className="w-[9px] h-[9px] rounded-full mt-[5px] shrink-0"
        style={{
          background: dot,
          boxShadow: `0 0 0 3px color-mix(in srgb, ${dot} 22%, transparent)`,
        }}
      />

      <div className="flex-1 min-w-0">
        {/* header row: rule id + badge + span id */}
        <div className="flex items-baseline gap-2 mb-[3px] flex-wrap">
          <span className="font-mono text-[11px] font-bold text-ink">{w.rule_id}</span>
          <span className={`px-1.5 py-px rounded font-mono text-[9px] font-semibold tracking-[0.04em] uppercase ${badgeClass}`}>
            {w.severity}
          </span>
          <span className="flex-1" />
          <span className="font-mono text-[10px] text-ink3 overflow-hidden text-ellipsis whitespace-nowrap max-w-[240px]">
            span: <span className="text-ink2">{w.span_id.slice(0, 16)}</span>
          </span>
        </div>

        {/* message */}
        <div className="text-[12.5px] text-ink leading-[1.45]">{w.message}</div>

        {/* trace link */}
        {w.trace_id && (
          <div className="font-mono text-[10px] text-accent-ink bg-accent-bg rounded-[5px] px-2 py-[3px] mt-1.5 inline-block">
            <span className="text-ink3">trace → </span>
            <a
              href={`/traces/${w.trace_id}`}
              className="text-inherit no-underline"
            >{w.trace_id.slice(0, 16)}…</a>
          </div>
        )}
      </div>
    </div>
  )
}

// ── LintPage ──────────────────────────────────────────────────────────────────

const KIND_LABELS: Record<string, string> = {
  n_plus_one:      'N+1 queries',
  slow_db:         'slow DB',
  chatty_http:     'chatty HTTP',
  large_payload:   'large payload',
  cache_miss_storm:'cache miss storm',
  tracing_gap:     'tracing gap',
  error_chain:     'error chain',
  serial_promise:  'serial promises',
  synchronous_io:  'synchronous I/O',
}

function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind.replace(/_/g, ' ')
}

export default function LintPage() {
  const [warnings, setWarnings]         = useState<LintWarning[]>([])
  const [traceIssues, setTraceIssues]   = useState<TraceIssue[]>([])
  const [loading, setLoading]           = useState(true)

  async function load() {
    try {
      const [lintRes, issueRes] = await Promise.all([
        api.lint.list(),
        api.issues.list(),
      ])
      setWarnings(lintRes.data ?? [])
      setTraceIssues(issueRes.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])
  useWS(ev => { if (ev.type === 'span') load() })

  const errors   = warnings.filter(w => w.severity === 'error').length
  const warnCnt  = warnings.filter(w => w.severity === 'warning').length
  const infoCnt  = warnings.filter(w => w.severity === 'info').length

  const reqAttr  = warnings.filter(w => w.severity === 'error').length
  const semconvW = warnings.filter(w => w.severity === 'warning').length

  // Aggregate detector issues by kind.
  const kindMap = new Map<string, { count: number; first: TraceIssue }>()
  for (const issue of traceIssues) {
    const existing = kindMap.get(issue.kind)
    if (!existing) {
      kindMap.set(issue.kind, { count: 1, first: issue })
    } else {
      existing.count++
    }
  }
  const detectorKinds = Array.from(kindMap.entries())
    .map(([kind, { count, first }]) => ({ kind, count, first }))
    .sort((a, b) => b.count - a.count)

  return (
    <div className="flex flex-col h-full overflow-hidden bg-[var(--bg)]">
      {/* panel head */}
      <div className="px-[18px] py-3 border-b border-line bg-surface flex items-center gap-3 shrink-0">
        <div className="flex-1">
          <div className="font-sans text-[13px] font-semibold text-ink tracking-[-0.01em]">Lint warnings</div>
          <div className="font-mono text-[10px] text-ink3 mt-0.5">OTel semantic conventions · N+1 detector</div>
        </div>
        <div className="flex gap-1.5">
          {errors > 0 && (
            <span className="px-[7px] py-0.5 rounded font-mono text-[9px] font-semibold tracking-[0.04em] uppercase text-danger-ink bg-danger-bg">
              {errors} error{errors !== 1 ? 's' : ''}
            </span>
          )}
          {warnCnt > 0 && (
            <span className="px-[7px] py-0.5 rounded font-mono text-[9px] font-semibold tracking-[0.04em] uppercase text-warn-ink bg-warn-bg">
              {warnCnt} warn{warnCnt !== 1 ? 's' : ''}
            </span>
          )}
          {infoCnt > 0 && (
            <span className="px-[7px] py-0.5 rounded font-mono text-[9px] font-semibold tracking-[0.04em] uppercase text-ink3 bg-chip-bg">
              {infoCnt} info
            </span>
          )}
        </div>
      </div>

      {/* summary band — 2-row grid, fills column-by-column */}
      <div className="px-[18px] py-3.5 grid grid-rows-[repeat(2,auto)] grid-flow-col auto-cols-fr gap-3 shrink-0 bg-surface border-b border-line">
        <SummaryStat
          tone="ok"
          big={warnings.length}
          label="total lint warnings"
          sub={loading ? 'loading…' : `${warnings.length} total`}
        />
        <SummaryStat
          tone="danger"
          big={reqAttr}
          label="missing required attr"
          sub={reqAttr > 0 ? warnings.find(w => w.severity === 'error')?.rule_id ?? '—' : 'none'}
        />
        <SummaryStat
          tone="neutral"
          big={semconvW}
          label="semconv warnings"
          sub={semconvW > 0 ? 'db.system, rpc…' : 'none'}
        />
        {detectorKinds.map(({ kind, count, first }) => (
          <SummaryStat
            key={kind}
            tone="warn"
            big={count}
            label={kindLabel(kind)}
            sub={first?.example_span_id ? first.example_span_id.slice(0, 10) + '…' : 'trace'}
          />
        ))}
      </div>

      {/* warning list */}
      <div className="flex-1 overflow-auto bg-surface">
        {loading && (
          <div className="px-[18px] py-8 font-mono text-xs text-ink3">Loading…</div>
        )}
        {!loading && warnings.length === 0 && (
          <EmptyState
            title="All clean!"
            hint="No semconv violations or N+1s detected."
            glyph={
              <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                <circle cx="16" cy="16" r="11" stroke="var(--ok)" strokeWidth="1.5" opacity="0.7" />
                <path d="M11 16.5 L14.5 20 L21 13" stroke="var(--ok)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            }
          />
        )}
        {!loading && warnings.map((w, i) => (
          <LintRow key={`${w.rule_id}-${w.span_id}-${i}`} w={w} last={i === warnings.length - 1} />
        ))}
      </div>
    </div>
  )
}
