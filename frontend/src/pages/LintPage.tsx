import { useEffect, useState } from 'react'
import { api, LintWarning } from '@/lib/api'
import { useWS } from '@/lib/ws'
import EmptyState from '@/components/EmptyState'

// ── Severity helpers ──────────────────────────────────────────────────────────

function sevDot(sev: string) {
  if (sev === 'error') return 'var(--danger)'
  if (sev === 'warning') return 'var(--warn)'
  return 'var(--ink3)'
}

function sevBadgeBg(sev: string) {
  if (sev === 'error') return 'var(--danger-bg)'
  if (sev === 'warning') return 'var(--warn-bg)'
  return 'var(--surface2)'
}

function sevBadgeInk(sev: string) {
  if (sev === 'error') return 'var(--danger-ink)'
  if (sev === 'warning') return 'var(--warn-ink)'
  return 'var(--ink2)'
}

// ── SummaryStat ───────────────────────────────────────────────────────────────

interface SummaryStatProps {
  tone: 'danger' | 'warn' | 'ok' | 'neutral'
  big: string | number
  label: string
  sub: string
}

function SummaryStat({ tone, big, label, sub }: SummaryStatProps) {
  const colors = {
    danger:  'var(--danger)',
    warn:    'var(--warn)',
    ok:      'var(--ok)',
    neutral: 'var(--ink3)',
  }
  return (
    <div style={{
      flex: 1,
      padding: '10px 12px',
      border: '1px solid var(--line)',
      borderRadius: 8,
      background: 'var(--surface2)',
    }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
        <span style={{
          width: 8, height: 8, borderRadius: 8,
          background: colors[tone], alignSelf: 'center', flexShrink: 0,
        }} />
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 20, fontWeight: 700,
          color: 'var(--ink)',
          lineHeight: 1,
        }}>{big}</span>
      </div>
      <div style={{
        fontFamily: 'var(--font-sans)',
        fontSize: 11, color: 'var(--ink2)',
        marginTop: 4,
      }}>{label}</div>
      <div style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 10, color: 'var(--ink3)',
        marginTop: 2,
      }}>{sub}</div>
    </div>
  )
}

// ── LintRow ───────────────────────────────────────────────────────────────────

function LintRow({ w, last }: { w: LintWarning; last: boolean }) {
  const dot = sevDot(w.severity)
  const badgeBg = sevBadgeBg(w.severity)
  const badgeInk = sevBadgeInk(w.severity)

  return (
    <div style={{
      padding: '12px 18px',
      borderBottom: last ? 'none' : '1px solid var(--line2)',
      display: 'flex',
      gap: 12,
      alignItems: 'flex-start',
    }}>
      {/* severity dot */}
      <span style={{
        width: 9, height: 9, borderRadius: 9,
        marginTop: 5, background: dot,
        flexShrink: 0,
        boxShadow: `0 0 0 3px color-mix(in srgb, ${dot} 22%, transparent)`,
      }} />

      <div style={{ flex: 1, minWidth: 0 }}>
        {/* header row: rule id + badge + span id */}
        <div style={{
          display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 3,
          flexWrap: 'wrap',
        }}>
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11, fontWeight: 700,
            color: 'var(--ink)',
          }}>{w.rule_id}</span>
          <span style={{
            padding: '1px 6px', borderRadius: 4,
            fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
            letterSpacing: '0.04em', textTransform: 'uppercase',
            color: badgeInk, background: badgeBg,
          }}>{w.severity}</span>
          <span style={{ flex: 1 }} />
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10, color: 'var(--ink3)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 240,
          }}>
            span: <span style={{ color: 'var(--ink2)' }}>{w.span_id.slice(0, 16)}</span>
          </span>
        </div>

        {/* message */}
        <div style={{
          fontSize: 12.5,
          color: 'var(--ink)',
          lineHeight: 1.45,
        }}>{w.message}</div>

        {/* trace link */}
        {w.trace_id && (
          <div style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10, color: 'var(--accent-ink)',
            background: 'var(--accent-bg)',
            borderRadius: 5, padding: '3px 8px',
            marginTop: 6, display: 'inline-block',
          }}>
            <span style={{ color: 'var(--ink3)' }}>trace → </span>
            <a
              href={`/traces/${w.trace_id}`}
              style={{ color: 'inherit', textDecoration: 'none' }}
            >{w.trace_id.slice(0, 16)}…</a>
          </div>
        )}
      </div>
    </div>
  )
}

// ── LintPage ──────────────────────────────────────────────────────────────────

export default function LintPage() {
  const [warnings, setWarnings] = useState<LintWarning[]>([])
  const [loading, setLoading]   = useState(true)

  async function load() {
    try {
      const res = await api.lint.list()
      setWarnings(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])
  useWS(ev => { if (ev.type === 'span') load() })

  const errors   = warnings.filter(w => w.severity === 'error').length
  const warnCnt  = warnings.filter(w => w.severity === 'warning').length
  const infoCnt  = warnings.filter(w => w.severity === 'info').length

  const n1Count  = warnings.filter(w => w.rule_id === 'n_plus_one').length
  const reqAttr  = warnings.filter(w => w.severity === 'error').length
  const semconvW = warnings.filter(w => w.severity === 'warning' && w.rule_id !== 'n_plus_one').length
  const firstN1  = warnings.find(w => w.rule_id === 'n_plus_one')

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      height: '100%',
      overflow: 'hidden',
      background: 'var(--bg)',
    }}>
      {/* panel head */}
      <div style={{
        padding: '12px 18px',
        borderBottom: '1px solid var(--line)',
        background: 'var(--surface)',
        display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0,
      }}>
        <div style={{ flex: 1 }}>
          <div style={{
            fontFamily: 'var(--font-sans)',
            fontSize: 13, fontWeight: 600,
            color: 'var(--ink)',
            letterSpacing: '-0.01em',
          }}>Lint warnings</div>
          <div style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10, color: 'var(--ink3)',
            marginTop: 2,
          }}>OTel semantic conventions · N+1 detector</div>
        </div>
        <div style={{ display: 'flex', gap: 6 }}>
          {errors > 0 && (
            <span style={{
              padding: '2px 7px', borderRadius: 4,
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase',
              color: 'var(--danger-ink)', background: 'var(--danger-bg)',
            }}>{errors} error{errors !== 1 ? 's' : ''}</span>
          )}
          {warnCnt > 0 && (
            <span style={{
              padding: '2px 7px', borderRadius: 4,
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase',
              color: 'var(--warn-ink)', background: 'var(--warn-bg)',
            }}>{warnCnt} warn{warnCnt !== 1 ? 's' : ''}</span>
          )}
          {infoCnt > 0 && (
            <span style={{
              padding: '2px 7px', borderRadius: 4,
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase',
              color: 'var(--ink3)', background: 'var(--chip-bg)',
            }}>{infoCnt} info</span>
          )}
        </div>
      </div>

      {/* summary band */}
      <div style={{
        padding: '14px 18px',
        display: 'flex', gap: 12, flexShrink: 0,
        background: 'var(--surface)',
        borderBottom: '1px solid var(--line)',
      }}>
        <SummaryStat
          tone="danger"
          big={reqAttr}
          label="missing required attr"
          sub={reqAttr > 0 ? warnings.find(w => w.severity === 'error')?.rule_id ?? '—' : 'none'}
        />
        <SummaryStat
          tone="warn"
          big={n1Count}
          label="N+1 detected"
          sub={firstN1 ? firstN1.span_id.slice(0, 12) + '…' : 'none'}
        />
        <SummaryStat
          tone="neutral"
          big={semconvW}
          label="semconv warnings"
          sub={semconvW > 0 ? 'db.system, rpc…' : 'none'}
        />
        <SummaryStat
          tone="ok"
          big={warnings.length}
          label="total warnings"
          sub={loading ? 'loading…' : `${warnings.length} total`}
        />
      </div>

      {/* warning list */}
      <div style={{ flex: 1, overflow: 'auto', background: 'var(--surface)' }}>
        {loading && (
          <div style={{
            padding: '32px 18px',
            fontFamily: 'var(--font-mono)', fontSize: 12,
            color: 'var(--ink3)',
          }}>Loading…</div>
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
