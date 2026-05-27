import { useEffect, useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, Session, LintWarning } from '@/lib/api'
import { readDiffHistory, pushDiffHistory, type DiffHistoryEntry } from '@/lib/diff-history'

// ── types ─────────────────────────────────────────────────────────────────────

type SessionFilter = 'all' | 'branches' | 'adhoc' | 'hot'

function isBranch(label: string) { return label.includes('/') }

function fmtDeltaMs(ns: number): string {
  const ms = Math.round(ns / 1_000_000)
  return (ms > 0 ? '+' : '') + ms + 'ms'
}

function fmtRelativeMs(atMs: number): string {
  const diff = Date.now() - atMs
  if (diff < 60_000) return `${Math.round(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`
  return `${Math.round(diff / 86_400_000)}d ago`
}

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtRelative(ns: number): string {
  const diffMs = Date.now() - ns / 1_000_000
  if (diffMs < 5_000) return 'just now'
  if (diffMs < 60_000) return `${Math.round(diffMs / 1000)}s ago`
  if (diffMs < 3_600_000) return `${Math.round(diffMs / 60_000)}m ago`
  if (diffMs < 86_400_000) return `${Math.round(diffMs / 3_600_000)}h ago`
  return `${Math.round(diffMs / 86_400_000)}d ago`
}

function fmtAbsolute(ns: number): string {
  return new Date(ns / 1_000_000).toLocaleString(undefined, { hour12: false })
}

// ── atom components ───────────────────────────────────────────────────────────

function StarButton({ on, onClick, title }: { on: boolean; onClick: () => void; title: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      style={{
        width: 28, height: 28, borderRadius: 6, padding: 0,
        background: on ? 'color-mix(in oklch, var(--warn) 30%, var(--surface))' : 'transparent',
        border: on ? '1px solid var(--warn)' : '1px solid var(--line)',
        cursor: 'pointer', outline: 'none',
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        flexShrink: 0,
      }}
    >
      <svg width="14" height="14" viewBox="0 0 14 14">
        <path
          d="M7 1.5l1.7 3.5 3.8.5-2.8 2.7.7 3.8L7 10.2 3.6 12l.7-3.8L1.5 5.5l3.8-.5L7 1.5z"
          fill={on ? 'var(--warn)' : 'none'}
          stroke={on ? 'var(--warn-ink)' : 'var(--ink3)'}
          strokeWidth="1"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  )
}

function DotPill({
  tone = 'neutral',
  children,
}: {
  tone?: 'neutral' | 'ok' | 'accent' | 'warn' | 'danger'
  children: React.ReactNode
}) {
  const tones: Record<string, { bg: string; fg: string; bd: string }> = {
    neutral: { bg: 'var(--surface2)', fg: 'var(--ink2)', bd: 'var(--line)' },
    ok: {
      bg: 'color-mix(in oklch, var(--ok) 22%, var(--surface))',
      fg: 'var(--ok-ink)', bd: 'color-mix(in oklch, var(--ok) 40%, transparent)',
    },
    accent: {
      bg: 'color-mix(in oklch, var(--accent) 18%, var(--surface))',
      fg: 'var(--accent-ink)', bd: 'color-mix(in oklch, var(--accent) 40%, transparent)',
    },
    warn: {
      bg: 'color-mix(in oklch, var(--warn) 28%, var(--surface))',
      fg: 'var(--warn-ink)', bd: 'color-mix(in oklch, var(--warn) 50%, transparent)',
    },
    danger: {
      bg: 'color-mix(in oklch, var(--danger) 28%, var(--surface))',
      fg: 'var(--danger-ink)', bd: 'color-mix(in oklch, var(--danger) 50%, transparent)',
    },
  }
  const t = tones[tone]
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 3,
      padding: '1px 6px', borderRadius: 10,
      fontFamily: 'var(--font-mono)', fontSize: 9.5, fontWeight: 600,
      background: t.bg, color: t.fg, border: `1px solid ${t.bd}`,
      whiteSpace: 'nowrap', lineHeight: 1.4,
    }}>
      {children}
    </span>
  )
}

function Btn({
  tone = 'default',
  disabled,
  onClick,
  children,
  small,
}: {
  tone?: 'default' | 'primary' | 'ghost' | 'danger'
  disabled?: boolean
  onClick?: () => void
  children: React.ReactNode
  small?: boolean
}) {
  const tones: Record<string, { bg: string; fg: string; bd: string }> = {
    default: { bg: 'var(--surface)', fg: 'var(--ink)', bd: 'var(--line)' },
    primary: { bg: 'var(--accent)', fg: 'var(--surface)', bd: 'var(--accent)' },
    ghost: { bg: 'transparent', fg: 'var(--ink2)', bd: 'var(--line)' },
    danger: { bg: 'transparent', fg: 'var(--danger-ink)', bd: 'var(--danger)' },
  }
  const t = tones[tone]
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{
        display: 'inline-flex', alignItems: 'center', gap: 6,
        padding: small ? '0 10px' : '0 14px',
        height: small ? 26 : 30,
        borderRadius: 6,
        background: disabled ? 'var(--surface2)' : t.bg,
        color: disabled ? 'var(--ink3)' : t.fg,
        border: `1px solid ${disabled ? 'var(--line)' : t.bd}`,
        fontFamily: 'var(--font-sans)', fontSize: 12, fontWeight: 600,
        cursor: disabled ? 'not-allowed' : 'pointer', outline: 'none',
        whiteSpace: 'nowrap',
      }}
    >
      {children}
    </button>
  )
}

function NumCell({ label, value, hot }: { label: string; value: string | number; hot?: boolean }) {
  return (
    <div>
      <div style={{
        fontFamily: 'var(--font-serif)', fontSize: 18, fontWeight: 600,
        color: hot ? 'var(--danger-ink)' : 'var(--ink)', lineHeight: 1.1,
      }}>
        {value}
      </div>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--ink3)',
        textTransform: 'uppercase', letterSpacing: '0.14em', marginTop: 2,
      }}>
        {label}
      </div>
    </div>
  )
}

// ── import modal ──────────────────────────────────────────────────────────────

function ImportIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
      <path d="M6 1v7M3.5 5.5L6 8l2.5-2.5" stroke="currentColor" strokeWidth="1.4"
        strokeLinecap="round" strokeLinejoin="round" />
      <path d="M1.5 10h9" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}

function ImportModal({ onClose, onImported }: { onClose: () => void; onImported: () => void }) {
  const [label, setLabel] = useState('')
  const [format, setFormat] = useState('auto')
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>('idle')
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  async function handleImport() {
    if (!file) { setError('Select a file first'); return }
    setStatus('loading')
    setError('')
    try {
      const text = await file.text()
      const res = await api.sessions.import(label || file.name.replace(/\.[^.]+$/, ''), format, text)
      const r = res.data
      alert(`Imported "${r.session.label}" — ${r.trace_count} traces, ${r.span_count} spans`)
      onImported()
      onClose()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e))
      setStatus('error')
    }
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault()
    setDragging(false)
    const f = e.dataTransfer.files[0]
    if (f) setFile(f)
  }

  const inputStyle: React.CSSProperties = {
    display: 'block', width: '100%', padding: '6px 10px',
    fontFamily: 'var(--font-mono)', fontSize: 12,
    background: 'var(--surface)', border: '1px solid var(--line)',
    borderRadius: 6, color: 'var(--ink)', outline: 'none',
    boxSizing: 'border-box',
  }

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 100,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'rgba(0,0,0,0.45)',
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        width: 420, background: 'var(--surface)', border: '1px solid var(--line)',
        borderRadius: 12, padding: '22px 24px', display: 'flex', flexDirection: 'column', gap: 16,
        boxShadow: '0 16px 48px -12px rgba(0,0,0,.35)',
      }}>
        {/* header */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{
            width: 28, height: 28, borderRadius: 8,
            background: 'color-mix(in oklch, var(--accent) 18%, var(--surface))',
            border: '1px solid color-mix(in oklch, var(--accent) 35%, transparent)',
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            color: 'var(--accent-ink)',
          }}>
            <ImportIcon />
          </span>
          <span style={{ fontFamily: 'var(--font-sans)', fontSize: 15, fontWeight: 700, color: 'var(--ink)' }}>
            Import trace
          </span>
          <div style={{ flex: 1 }} />
          <button type="button" onClick={onClose} style={{
            background: 'none', border: 'none', cursor: 'pointer',
            color: 'var(--ink3)', fontSize: 18, lineHeight: 1, padding: 2,
          }}>×</button>
        </div>

        {/* drop zone */}
        <div
          onDragOver={e => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
          style={{
            border: `2px dashed ${dragging ? 'var(--accent)' : file ? 'var(--ok)' : 'var(--line)'}`,
            borderRadius: 8, padding: '18px 16px', textAlign: 'center' as const,
            cursor: 'pointer',
            background: dragging ? 'color-mix(in oklch, var(--accent) 8%, var(--surface))' :
              file ? 'color-mix(in oklch, var(--ok) 8%, var(--surface))' : 'var(--surface2)',
            transition: 'border-color 0.15s, background 0.15s',
          }}
        >
          <input ref={fileRef} type="file" accept=".json" style={{ display: 'none' }}
            onChange={e => e.target.files?.[0] && setFile(e.target.files[0])} />
          {file ? (
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--ok-ink)', fontWeight: 600 }}>
              {file.name} ({(file.size / 1024).toFixed(0)} KB)
            </span>
          ) : (
            <>
              <div style={{ fontFamily: 'var(--font-sans)', fontSize: 13, color: 'var(--ink2)', marginBottom: 4 }}>
                Drop OTLP JSON or Jaeger JSON here
              </div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)' }}>
                or click to browse
              </div>
            </>
          )}
        </div>

        {/* label */}
        <div>
          <label style={{
            display: 'block', fontFamily: 'var(--font-mono)', fontSize: 10,
            textTransform: 'uppercase', letterSpacing: '0.12em',
            color: 'var(--ink3)', marginBottom: 5,
          }}>
            session label
          </label>
          <input
            type="text"
            value={label}
            onChange={e => setLabel(e.target.value)}
            placeholder={file ? file.name.replace(/\.[^.]+$/, '') : 'prod-baseline'}
            style={inputStyle}
          />
        </div>

        {/* format */}
        <div>
          <label style={{
            display: 'block', fontFamily: 'var(--font-mono)', fontSize: 10,
            textTransform: 'uppercase', letterSpacing: '0.12em',
            color: 'var(--ink3)', marginBottom: 5,
          }}>
            format
          </label>
          <select value={format} onChange={e => setFormat(e.target.value)} style={inputStyle}>
            <option value="auto">Auto-detect</option>
            <option value="otlp">OTLP JSON (otelcol export)</option>
            <option value="jaeger">Jaeger JSON</option>
          </select>
        </div>

        {/* error */}
        {status === 'error' && (
          <div style={{
            padding: '8px 10px', borderRadius: 6,
            background: 'color-mix(in oklch, var(--danger) 12%, var(--surface))',
            border: '1px solid color-mix(in oklch, var(--danger) 35%, transparent)',
            fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--danger-ink)',
          }}>
            {error}
          </div>
        )}

        {/* actions */}
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <Btn tone="ghost" onClick={onClose}>Cancel</Btn>
          <Btn tone="primary" disabled={!file || status === 'loading'} onClick={handleImport}>
            {status === 'loading' ? 'importing…' : (
              <><ImportIcon /> Import as baseline</>
            )}
          </Btn>
        </div>
      </div>
    </div>
  )
}

// ── how-to-diff explainer ─────────────────────────────────────────────────────

function HowToDiff() {
  const steps = [
    {
      n: '1', title: 'Mark a baseline',
      body: 'Click ★ on the session you trust — usually main. That run becomes your reference.',
    },
    {
      n: '2', title: 'Switch and re-run',
      body: 'Activate a new session, hit the same endpoint after editing. Spans land here.',
    },
    {
      n: '3', title: 'Open the diff',
      body: 'Select a session ↔ baseline pair and click Compare. Span shapes and N+1s side-by-side.',
    },
  ]
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 14, marginBottom: 20 }}>
      {steps.map(s => (
        <div key={s.n} style={{
          background: 'var(--surface)', border: '1px solid var(--line)', borderRadius: 10,
          padding: '14px 16px',
        }}>
          <div style={{
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
            width: 24, height: 24, borderRadius: 24,
            background: 'var(--accent)', color: 'var(--surface)',
            fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 700, marginBottom: 8,
          }}>
            {s.n}
          </div>
          <div style={{
            fontFamily: 'var(--font-sans)', fontSize: 13, fontWeight: 600,
            color: 'var(--ink)', marginBottom: 4,
          }}>
            {s.title}
          </div>
          <div style={{ fontFamily: 'var(--font-sans)', fontSize: 12, color: 'var(--ink2)', lineHeight: 1.5 }}>
            {s.body}
          </div>
        </div>
      ))}
    </div>
  )
}

// ── session row ───────────────────────────────────────────────────────────────

function SessionRow({
  s, isActive, isBaseline, isCompare,
  onBaseline, onCompare, onActivate, onDelete,
}: {
  s: Session
  isActive: boolean
  isBaseline: boolean
  isCompare: boolean
  onBaseline: (id: string) => void
  onCompare: (id: string | null) => void
  onActivate: (id: string) => void
  onDelete: (id: string) => void
}) {
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: '34px minmax(0,1fr) 100px 64px 64px 168px',
      gap: 10, alignItems: 'center', padding: '12px 14px',
      borderBottom: '1px solid var(--line2)',
      background:
        isBaseline ? 'color-mix(in oklch, var(--warn) 8%, var(--surface))'
          : isCompare ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
          : isActive ? 'var(--surface)'
          : 'transparent',
      borderLeft:
        isBaseline ? '2px solid var(--warn)'
          : isCompare ? '2px solid var(--accent)'
          : isActive ? '2px solid var(--ok)'
          : '2px solid transparent',
    }}>
      {/* star / baseline */}
      <StarButton
        on={isBaseline}
        onClick={() => onBaseline(s.id)}
        title={isBaseline ? 'Baseline (click to unpin)' : 'Mark as baseline'}
      />

      {/* name + pills */}
      <div style={{ minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
          {s.is_imported && (
            <span title="Imported trace" style={{
              display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
              width: 16, height: 16, borderRadius: 4, flexShrink: 0,
              background: 'color-mix(in oklch, var(--accent) 18%, var(--surface))',
              color: 'var(--accent-ink)',
            }}>
              <ImportIcon />
            </span>
          )}
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 13, fontWeight: 600, color: 'var(--ink)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>
            {s.label || s.id.slice(0, 8)}
          </span>
          {isActive && <DotPill tone="ok">● active</DotPill>}
          {isBaseline && <DotPill tone="warn">★ baseline</DotPill>}
        </div>
        <div style={{
          fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)', marginTop: 3,
        }} title={fmtAbsolute(s.created_at)}>
          created {fmtRelative(s.created_at)}
        </div>
      </div>

      {/* stats */}
      <NumCell label="traces" value={s.trace_count} />
      <NumCell label="spans" value={s.span_count} />

      {/* actions col */}
      <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
        {isCompare ? (
          <Btn small tone="ghost" onClick={() => onCompare(null)}>− deselect</Btn>
        ) : isBaseline ? (
          <Btn small disabled>baseline</Btn>
        ) : (
          <Btn small onClick={() => onCompare(s.id)}>+ compare</Btn>
        )}
        {!isActive ? (
          <Btn small tone="ghost" onClick={() => onActivate(s.id)}>switch</Btn>
        ) : null}
        {!isActive ? (
          <Btn small tone="danger" onClick={() => {
            if (confirm(`Delete session "${s.label || s.id.slice(0, 8)}"?`)) onDelete(s.id)
          }}>
            ×
          </Btn>
        ) : null}
      </div>
    </div>
  )
}

// ── diff selection mini (sidebar) ─────────────────────────────────────────────

function DiffSelectionMini({ baseline, compare }: { baseline?: Session; compare?: Session }) {
  return (
    <div style={{
      padding: '8px 10px', borderRadius: 8, border: '1px solid var(--line)',
      background: 'var(--surface)', display: 'flex', flexDirection: 'column', gap: 6,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span style={{
          width: 16, height: 16, borderRadius: 4, display: 'inline-flex',
          alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          background: 'color-mix(in oklch, var(--warn) 30%, var(--surface))',
          color: 'var(--warn-ink)', fontSize: 10, fontWeight: 700,
        }}>★</span>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 11,
          color: baseline ? 'var(--ink)' : 'var(--ink3)',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1,
        }}>
          {baseline ? (baseline.label || baseline.id.slice(0, 8)) : 'no baseline pinned'}
        </span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span style={{
          width: 16, height: 16, borderRadius: 4, display: 'inline-flex',
          alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          background: 'color-mix(in oklch, var(--accent) 30%, var(--surface))',
          color: 'var(--accent-ink)', fontSize: 10, fontWeight: 700,
        }}>+</span>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 11,
          color: compare ? 'var(--ink)' : 'var(--ink3)',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1,
        }}>
          {compare ? (compare.label || compare.id.slice(0, 8)) : 'pick a session'}
        </span>
      </div>
    </div>
  )
}

// ── compare bar ───────────────────────────────────────────────────────────────

function CompareBar({ baseline, compare }: { baseline?: Session; compare?: Session }) {
  const navigate = useNavigate()
  const canDiff = baseline && compare && baseline.id !== compare.id

  function handleCompare() {
    if (!canDiff) return
    pushDiffHistory({
      baselineId: baseline.id,
      baselineLabel: baseline.label || baseline.id.slice(0, 8),
      compareId: compare.id,
      compareLabel: compare.label || compare.id.slice(0, 8),
      at: Date.now(),
    })
    navigate(`/diff?baseline=${baseline.id}&compare=${compare.id}`)
  }

  return (
    <div style={{
      borderTop: '1px solid var(--line)',
      background: 'var(--surface)',
      padding: '10px 18px',
      display: 'flex', alignItems: 'center', gap: 14, flexShrink: 0,
    }}>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink3)',
        textTransform: 'uppercase', letterSpacing: '0.14em',
      }}>
        diff
      </div>
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
        padding: '4px 10px', borderRadius: 6,
        background: 'color-mix(in oklch, var(--warn) 18%, var(--surface))',
        color: 'var(--warn-ink)',
        border: '1px solid color-mix(in oklch, var(--warn) 35%, transparent)',
      }}>
        ★ {baseline ? (baseline.label || baseline.id.slice(0, 8)) : '— pick baseline —'}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--ink3)' }}>→</span>
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
        padding: '4px 10px', borderRadius: 6,
        background: 'color-mix(in oklch, var(--accent) 16%, var(--surface))',
        color: 'var(--accent-ink)',
        border: '1px solid color-mix(in oklch, var(--accent) 35%, transparent)',
      }}>
        + {compare ? (compare.label || compare.id.slice(0, 8)) : '— pick comparison —'}
      </span>
      <div style={{ flex: 1 }} />
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)' }}>
        or run{' '}
        <span style={{
          background: 'var(--surface2)', padding: '1px 6px', borderRadius: 4, color: 'var(--ink2)',
        }}>
          spaniel diff --baseline {baseline ? (baseline.label || 'main') : 'main'}
        </span>
      </span>
      <Btn tone="primary" disabled={!canDiff} onClick={handleCompare}>
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" style={{ display: 'block' }}>
          <path d="M2.5 4h5M5 2l-2.5 2L5 6" stroke="currentColor" strokeWidth="1.4"
            strokeLinecap="round" strokeLinejoin="round" />
          <path d="M11.5 10h-5M9 8l2.5 2L9 12" stroke="currentColor" strokeWidth="1.4"
            strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        Compare sessions
      </Btn>
    </div>
  )
}

// ── main page ─────────────────────────────────────────────────────────────────

export default function Sessions() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeId, setActiveId] = useState<string>('')
  const [baselineId, setBaselineId] = useState<string | null>(null)
  const [compareId, setCompareId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [showImport, setShowImport] = useState(false)
  const [filter, setFilter] = useState<SessionFilter>('all')
  const [warningSids, setWarningSids] = useState<Set<string>>(new Set())
  const [diffHistory, setDiffHistory] = useState<DiffHistoryEntry[]>([])

  const load = useCallback(async () => {
    const [sessRes, activeRes, lintRes] = await Promise.all([
      api.sessions.list(),
      api.sessions.getActive(),
      api.lint.list(),
    ])
    const list = sessRes.data ?? []
    setSessions(list)
    setActiveId(activeRes.data?.id ?? '')
    const bl = list.find(s => s.is_baseline)
    if (bl) setBaselineId(bl.id)
    const sids = new Set((lintRes.data ?? []).map((w: LintWarning) => w.session_id))
    setWarningSids(sids)
    setDiffHistory(readDiffHistory())
    setLoading(false)
  }, [])

  useEffect(() => { load() }, [load])

  async function handleNew() {
    setCreating(true)
    try {
      const label = prompt('Session label (leave blank for timestamp auto-name):')
      const res = await api.sessions.create(label ?? undefined)
      const newId = res.data.id
      await api.sessions.activate(newId)
      setActiveId(newId)
      await load()
    } finally {
      setCreating(false)
    }
  }

  async function handleBaseline(id: string) {
    const isNowBaseline = baselineId !== id
    await api.sessions.baseline(id, isNowBaseline)
    if (!isNowBaseline) {
      setBaselineId(null)
    } else {
      if (compareId === id) setCompareId(null)
      setBaselineId(id)
    }
    await load()
  }

  async function handleActivate(id: string) {
    await api.sessions.activate(id)
    setActiveId(id)
    await load()
  }

  async function handleDelete(id: string) {
    await api.sessions.delete(id)
    if (compareId === id) setCompareId(null)
    if (baselineId === id) setBaselineId(null)
    await load()
  }

  function handleCompare(id: string | null) {
    if (id === baselineId) return
    setCompareId(id)
  }

  const filteredSessions = sessions.filter(s => {
    if (filter === 'branches') return isBranch(s.label)
    if (filter === 'adhoc') return !isBranch(s.label)
    if (filter === 'hot') return warningSids.has(s.id)
    return true
  })

  const branchCount = sessions.filter(s => isBranch(s.label)).length
  const adhocCount = sessions.filter(s => !isBranch(s.label)).length
  const hotCount = sessions.filter(s => warningSids.has(s.id)).length

  const baseline = sessions.find(s => s.id === baselineId)
  const compare = sessions.find(s => s.id === compareId)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {showImport && (
        <ImportModal onClose={() => setShowImport(false)} onImported={load} />
      )}
      <div style={{ flex: 1, display: 'flex', minHeight: 0 }}>

        {/* sidebar */}
        <aside style={{
          width: 200, padding: '18px 14px',
          borderRight: '1px solid var(--line)',
          background: 'var(--surface2)',
          display: 'flex', flexDirection: 'column', gap: 18,
          fontFamily: 'var(--font-sans)', flexShrink: 0,
        }}>
          {/* filter tabs */}
          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, textTransform: 'uppercase',
              letterSpacing: '0.14em', color: 'var(--ink3)', marginBottom: 8,
            }}>
              filter
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
              {([
                { key: 'all', label: 'all sessions', dot: 'var(--ink3)', count: sessions.length },
                { key: 'branches', label: 'branches', dot: 'var(--accent)', count: branchCount },
                { key: 'adhoc', label: 'scratch / ad-hoc', dot: 'var(--warn, #d97706)', count: adhocCount },
                { key: 'hot', label: 'with warnings', dot: 'var(--destructive, #c0392b)', count: hotCount },
              ] as const).map(item => (
                <button
                  key={item.key}
                  type="button"
                  data-testid={`filter-${item.key}`}
                  onClick={() => setFilter(item.key)}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 7,
                    width: '100%', textAlign: 'left',
                    padding: '5px 8px', borderRadius: 6, border: 'none', cursor: 'pointer',
                    background: filter === item.key ? 'var(--surface)' : 'transparent',
                    fontFamily: 'var(--font-mono)', fontSize: 11,
                    color: filter === item.key ? 'var(--ink)' : 'var(--ink2)',
                    fontWeight: filter === item.key ? 600 : 400,
                  }}
                >
                  <span style={{ width: 7, height: 7, borderRadius: 7, background: item.dot, flexShrink: 0 }} />
                  <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {item.label}
                  </span>
                  <span style={{ color: 'var(--ink3)', fontSize: 10 }}>{item.count}</span>
                </button>
              ))}
            </div>
          </div>

          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, textTransform: 'uppercase',
              letterSpacing: '0.14em', color: 'var(--ink3)', marginBottom: 8,
            }}>
              actions
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <div
                role="button"
                onClick={!creating ? handleNew : undefined}
                style={{
                  display: 'flex', alignItems: 'center', gap: 8,
                  padding: '5px 8px', borderRadius: 6,
                  fontSize: 12, color: 'var(--accent-ink)',
                  cursor: creating ? 'default' : 'pointer',
                  userSelect: 'none',
                  fontWeight: 600,
                }}
              >
                <span style={{ width: 7, height: 7, borderRadius: 7, background: 'var(--accent)' }} />
                {creating ? 'creating…' : '+ new session'}
              </div>
              <div
                role="button"
                onClick={() => setShowImport(true)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 8,
                  padding: '5px 8px', borderRadius: 6,
                  fontSize: 12, color: 'var(--ink2)',
                  cursor: 'pointer', userSelect: 'none',
                }}
              >
                <span style={{ display: 'inline-flex', color: 'var(--ink3)' }}><ImportIcon /></span>
                import trace
              </div>
            </div>
          </div>

          <div style={{ flex: 1 }} />

          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, textTransform: 'uppercase',
              letterSpacing: '0.14em', color: 'var(--ink3)', marginBottom: 8,
            }}>
              diff selection
            </div>
            <DiffSelectionMini baseline={baseline} compare={compare} />
          </div>
        </aside>

        {/* main */}
        <div style={{ flex: 1, overflow: 'hidden auto', display: 'flex', flexDirection: 'column' }}>

          {/* page header */}
          <div style={{ padding: '22px 24px 0' }}>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--ink3)',
              textTransform: 'uppercase', letterSpacing: '0.18em', marginBottom: 6,
            }}>
              spaniel sessions
            </div>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 14 }}>
              <h1 style={{
                margin: 0, fontFamily: 'var(--font-serif)', fontSize: 28, fontWeight: 600,
                letterSpacing: '-0.02em', color: 'var(--ink)',
              }}>
                Sessions
              </h1>
              <span style={{ fontFamily: 'var(--font-sans)', fontSize: 13, color: 'var(--ink2)' }}>
                Named windows of telemetry. Each branch usually gets its own.
              </span>
            </div>
          </div>

          {/* diff workflow explainer */}
          <div style={{ padding: '18px 24px 4px' }}>
            <HowToDiff />
          </div>

          {/* sessions table */}
          <div style={{ padding: '4px 24px' }}>
            {/* column header */}
            <div style={{
              display: 'grid',
              gridTemplateColumns: '34px minmax(0,1fr) 100px 64px 64px 168px',
              gap: 10, padding: '8px 14px',
              fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--ink3)',
              textTransform: 'uppercase', letterSpacing: '0.14em',
              background: 'var(--surface2)',
              borderRadius: '10px 10px 0 0',
              border: '1px solid var(--line)',
            }}>
              <div title="Mark baseline">★</div>
              <div>session · created</div>
              <div>traces</div>
              <div>spans</div>
              <div style={{ gridColumn: 'span 2', textAlign: 'right' }}>actions</div>
            </div>

            {loading ? (
              <div style={{
                padding: '24px 16px',
                fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--ink3)',
                background: 'var(--surface)',
                border: '1px solid var(--line)', borderTop: 'none',
                borderRadius: '0 0 10px 10px',
              }}>
                loading…
              </div>
            ) : filteredSessions.length === 0 ? (
              <div style={{
                padding: '32px 16px', textAlign: 'center',
                fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--ink3)',
                background: 'var(--surface)',
                border: '1px solid var(--line)', borderTop: 'none',
                borderRadius: '0 0 10px 10px',
              }}>
                {sessions.length === 0 ? 'no sessions yet' : 'no sessions match this filter'}
              </div>
            ) : (
              <div
                data-testid="sessions-table-body"
                style={{
                  background: 'var(--surface)',
                  border: '1px solid var(--line)', borderTop: 'none',
                  borderRadius: '0 0 10px 10px', overflow: 'hidden',
                }}
              >
                {filteredSessions.map(s => (
                  <SessionRow
                    key={s.id}
                    s={s}
                    isActive={s.id === activeId}
                    isBaseline={s.id === baselineId}
                    isCompare={s.id === compareId}
                    onBaseline={handleBaseline}
                    onCompare={handleCompare}
                    onActivate={handleActivate}
                    onDelete={handleDelete}
                  />
                ))}
              </div>
            )}
          </div>

          {/* recent diffs */}
          {diffHistory.length > 0 && (
            <div style={{ padding: '18px 24px 4px' }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, marginBottom: 8 }}>
                <h2 style={{
                  margin: 0, fontFamily: 'var(--font-serif)', fontSize: 16, fontWeight: 600, color: 'var(--ink)',
                }}>Recent diffs</h2>
                <span style={{ fontFamily: 'var(--font-sans)', fontSize: 11, color: 'var(--ink2)' }}>
                  cached locally · re-open any to compare again
                </span>
              </div>
              <div style={{
                background: 'var(--surface)', border: '1px solid var(--line)', borderRadius: 10,
                overflow: 'hidden',
              }}>
                {diffHistory.map((d, i) => (
                  <div key={i} style={{
                    display: 'grid',
                    gridTemplateColumns: 'minmax(0,1fr) 80px 80px 80px',
                    gap: 12, padding: '10px 16px', alignItems: 'center',
                    borderBottom: i < diffHistory.length - 1 ? '1px solid var(--line2)' : 'none',
                  }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11.5, color: 'var(--ink2)' }}>
                        {d.baselineLabel}
                      </span>
                      <span style={{ color: 'var(--ink3)' }}>→</span>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11.5, fontWeight: 600, color: 'var(--ink)' }}>
                        {d.compareLabel}
                      </span>
                      <a
                        href={`/diff?baseline=${d.baselineId}&compare=${d.compareId}`}
                        style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--accent-ink)', marginLeft: 4 }}
                      >
                        re-open
                      </a>
                    </div>
                    {d.deltaMs !== undefined ? (
                      <span style={{
                        fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
                        color: d.deltaMs < 0 ? '#3e6a3e' : d.deltaMs > 0 ? 'var(--danger-ink)' : 'var(--ink3)',
                      }}>
                        {fmtDeltaMs(d.deltaMs * 1_000_000)}
                      </span>
                    ) : <span />}
                    {d.deltaSpans !== undefined ? (
                      <span style={{
                        fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
                        color: d.deltaSpans < 0 ? '#3e6a3e' : d.deltaSpans > 0 ? 'var(--danger-ink)' : 'var(--ink3)',
                      }}>
                        {(d.deltaSpans > 0 ? '+' : '') + d.deltaSpans} spans
                      </span>
                    ) : <span />}
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10.5, color: 'var(--ink3)' }}>
                      {fmtRelativeMs(d.at)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div style={{ height: 80 }} />
        </div>
      </div>

      {/* sticky compare bar */}
      <CompareBar baseline={baseline} compare={compare} />
    </div>
  )
}
