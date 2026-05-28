import { useEffect, useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, Session, LintWarning } from '@/lib/api'
import { readDiffHistory, pushDiffHistory, type DiffHistoryEntry } from '@/lib/diff-history'
import EmptyState from '@/components/EmptyState'

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
      className={`w-7 h-7 rounded-md p-0 cursor-pointer outline-none inline-flex items-center justify-center shrink-0 ${
        on
          ? 'bg-[color-mix(in_oklch,var(--warn)_30%,var(--surface))] border border-warn'
          : 'bg-transparent border border-line'
      }`}
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
    <span
      className="inline-flex items-center gap-[3px] px-1.5 py-px rounded-[10px] font-mono text-[9.5px] font-semibold whitespace-nowrap leading-[1.4]"
      style={{ background: t.bg, color: t.fg, border: `1px solid ${t.bd}` }}
    >
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
      className={`inline-flex items-center gap-1.5 rounded-md font-sans text-xs font-semibold outline-none whitespace-nowrap ${
        small ? 'px-2.5 h-[26px]' : 'px-3.5 h-[30px]'
      } ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
      style={{
        background: disabled ? 'var(--surface2)' : t.bg,
        color: disabled ? 'var(--ink3)' : t.fg,
        border: `1px solid ${disabled ? 'var(--line)' : t.bd}`,
      }}
    >
      {children}
    </button>
  )
}

function NumCell({ label, value, hot }: { label: string; value: string | number; hot?: boolean }) {
  return (
    <div>
      <div className={`font-serif text-lg font-semibold leading-[1.1] ${hot ? 'text-danger-ink' : 'text-ink'}`}>
        {value}
      </div>
      <div className="font-mono text-[9px] text-ink3 uppercase tracking-[0.14em] mt-0.5">
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

  const inputClass = 'block w-full px-2.5 py-1.5 font-mono text-xs bg-surface border border-line rounded-md text-ink outline-none box-border'
  const labelClass = 'block font-mono text-[10px] uppercase tracking-[0.12em] text-ink3 mb-[5px]'

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/45"
      onClick={onClose}
    >
      <div
        onClick={e => e.stopPropagation()}
        className="w-[420px] bg-surface border border-line rounded-xl px-6 py-[22px] flex flex-col gap-4 shadow-[0_16px_48px_-12px_rgba(0,0,0,.35)]"
      >
        {/* header */}
        <div className="flex items-center gap-2">
          <span className="w-7 h-7 rounded-lg bg-[color-mix(in_oklch,var(--accent)_18%,var(--surface))] border border-[color-mix(in_oklch,var(--accent)_35%,transparent)] inline-flex items-center justify-center text-accent-ink">
            <ImportIcon />
          </span>
          <span className="font-sans text-[15px] font-bold text-ink">
            Import trace
          </span>
          <div className="flex-1" />
          <button
            type="button"
            onClick={onClose}
            className="bg-none border-none cursor-pointer text-ink3 text-lg leading-none p-0.5"
          >×</button>
        </div>

        {/* drop zone */}
        <div
          onDragOver={e => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
          className="rounded-lg px-4 py-[18px] text-center cursor-pointer transition-[border-color,background] duration-150"
          style={{
            border: `2px dashed ${dragging ? 'var(--accent)' : file ? 'var(--ok)' : 'var(--line)'}`,
            background: dragging ? 'color-mix(in oklch, var(--accent) 8%, var(--surface))' :
              file ? 'color-mix(in oklch, var(--ok) 8%, var(--surface))' : 'var(--surface2)',
          }}
        >
          <input
            ref={fileRef}
            type="file"
            accept=".json"
            className="hidden"
            onChange={e => e.target.files?.[0] && setFile(e.target.files[0])}
          />
          {file ? (
            <span className="font-mono text-xs text-ok-ink font-semibold">
              {file.name} ({(file.size / 1024).toFixed(0)} KB)
            </span>
          ) : (
            <>
              <div className="font-sans text-[13px] text-ink2 mb-1">
                Drop OTLP JSON or Jaeger JSON here
              </div>
              <div className="font-mono text-[10px] text-ink3">
                or click to browse
              </div>
            </>
          )}
        </div>

        {/* label */}
        <div>
          <label className={labelClass}>
            session label
          </label>
          <input
            type="text"
            value={label}
            onChange={e => setLabel(e.target.value)}
            placeholder={file ? file.name.replace(/\.[^.]+$/, '') : 'prod-baseline'}
            className={inputClass}
          />
        </div>

        {/* format */}
        <div>
          <label className={labelClass}>
            format
          </label>
          <select value={format} onChange={e => setFormat(e.target.value)} className={inputClass}>
            <option value="auto">Auto-detect</option>
            <option value="otlp">OTLP JSON (otelcol export)</option>
            <option value="jaeger">Jaeger JSON</option>
          </select>
        </div>

        {/* error */}
        {status === 'error' && (
          <div className="px-2.5 py-2 rounded-md bg-[color-mix(in_oklch,var(--danger)_12%,var(--surface))] border border-[color-mix(in_oklch,var(--danger)_35%,transparent)] font-mono text-[11px] text-danger-ink">
            {error}
          </div>
        )}

        {/* actions */}
        <div className="flex gap-2 justify-end">
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
    <div className="grid grid-cols-3 gap-3.5 mb-5">
      {steps.map(s => (
        <div key={s.n} className="bg-surface border border-line rounded-[10px] px-4 py-3.5">
          <div className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-[var(--accent)] text-surface font-mono text-xs font-bold mb-2">
            {s.n}
          </div>
          <div className="font-sans text-[13px] font-semibold text-ink mb-1">
            {s.title}
          </div>
          <div className="font-sans text-xs text-ink2 leading-[1.5]">
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
    <div
      className="grid gap-2.5 items-center px-3.5 py-3 border-b border-[var(--line2)]"
      style={{
        gridTemplateColumns: '34px minmax(0,1fr) 100px 64px 64px 168px',
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
      }}
    >
      {/* star / baseline */}
      <StarButton
        on={isBaseline}
        onClick={() => onBaseline(s.id)}
        title={isBaseline ? 'Baseline (click to unpin)' : 'Mark as baseline'}
      />

      {/* name + pills */}
      <div className="min-w-0">
        <div className="flex items-center gap-1.5 flex-wrap">
          {s.is_imported && (
            <span
              title="Imported trace"
              className="inline-flex items-center justify-center w-4 h-4 rounded shrink-0 bg-[color-mix(in_oklch,var(--accent)_18%,var(--surface))] text-accent-ink"
            >
              <ImportIcon />
            </span>
          )}
          <span className="font-mono text-[13px] font-semibold text-ink overflow-hidden text-ellipsis whitespace-nowrap">
            {s.label || s.id.slice(0, 8)}
          </span>
          {isActive && <DotPill tone="ok">● active</DotPill>}
          {isBaseline && <DotPill tone="warn">★ baseline</DotPill>}
        </div>
        <div
          className="font-mono text-[10px] text-ink3 mt-[3px]"
          title={fmtAbsolute(s.created_at)}
        >
          created {fmtRelative(s.created_at)}
        </div>
      </div>

      {/* stats */}
      <NumCell label="traces" value={s.trace_count} />
      <NumCell label="spans" value={s.span_count} />

      {/* actions col */}
      <div className="flex gap-1.5 justify-end flex-wrap">
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
    <div className="px-2.5 py-2 rounded-lg border border-line bg-surface flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <span className="w-4 h-4 rounded inline-flex items-center justify-center shrink-0 bg-[color-mix(in_oklch,var(--warn)_30%,var(--surface))] text-warn-ink text-[10px] font-bold">★</span>
        <span className={`font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 ${baseline ? 'text-ink' : 'text-ink3'}`}>
          {baseline ? (baseline.label || baseline.id.slice(0, 8)) : 'no baseline pinned'}
        </span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="w-4 h-4 rounded inline-flex items-center justify-center shrink-0 bg-[color-mix(in_oklch,var(--accent)_30%,var(--surface))] text-accent-ink text-[10px] font-bold">+</span>
        <span className={`font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 ${compare ? 'text-ink' : 'text-ink3'}`}>
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
    <div className="border-t border-line bg-surface px-[18px] py-2.5 flex items-center gap-3.5 shrink-0">
      <div className="font-mono text-[11px] text-ink3 uppercase tracking-[0.14em]">
        diff
      </div>
      <span className="font-mono text-xs font-semibold px-2.5 py-1 rounded-md bg-[color-mix(in_oklch,var(--warn)_18%,var(--surface))] text-warn-ink border border-[color-mix(in_oklch,var(--warn)_35%,transparent)]">
        ★ {baseline ? (baseline.label || baseline.id.slice(0, 8)) : '— pick baseline —'}
      </span>
      <span className="font-mono text-ink3">→</span>
      <span className="font-mono text-xs font-semibold px-2.5 py-1 rounded-md bg-[color-mix(in_oklch,var(--accent)_16%,var(--surface))] text-accent-ink border border-[color-mix(in_oklch,var(--accent)_35%,transparent)]">
        + {compare ? (compare.label || compare.id.slice(0, 8)) : '— pick comparison —'}
      </span>
      <div className="flex-1" />
      <span className="font-mono text-[10px] text-ink3">
        or run{' '}
        <span className="bg-surface2 px-1.5 py-px rounded text-ink2">
          spaniel diff --baseline {baseline ? (baseline.label || 'main') : 'main'}
        </span>
      </span>
      <Btn tone="primary" disabled={!canDiff} onClick={handleCompare}>
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" className="block">
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
    <div className="flex flex-col h-full overflow-hidden">
      {showImport && (
        <ImportModal onClose={() => setShowImport(false)} onImported={load} />
      )}
      <div className="flex-1 flex min-h-0">

        {/* sidebar */}
        <aside className="w-[200px] px-3.5 py-[18px] border-r border-line bg-surface2 flex flex-col gap-[18px] font-sans shrink-0">
          {/* filter tabs */}
          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              filter
            </div>
            <div className="flex flex-col gap-px">
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
                  className={`flex items-center gap-[7px] w-full text-left px-2 py-[5px] rounded-md border-0 cursor-pointer font-mono text-[11px] ${
                    filter === item.key ? 'bg-surface text-ink font-semibold' : 'bg-transparent text-ink2 font-normal'
                  }`}
                >
                  <span className="w-[7px] h-[7px] rounded-full shrink-0" style={{ background: item.dot }} />
                  <span className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
                    {item.label}
                  </span>
                  <span className="text-ink3 text-[10px]">{item.count}</span>
                </button>
              ))}
            </div>
          </div>

          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              actions
            </div>
            <div className="flex flex-col gap-0.5">
              <div
                role="button"
                onClick={!creating ? handleNew : undefined}
                className={`flex items-center gap-2 px-2 py-[5px] rounded-md text-xs text-accent-ink select-none font-semibold ${creating ? 'cursor-default' : 'cursor-pointer'}`}
              >
                <span className="w-[7px] h-[7px] rounded-full bg-[var(--accent)]" />
                {creating ? 'creating…' : '+ new session'}
              </div>
              <div
                role="button"
                onClick={() => setShowImport(true)}
                className="flex items-center gap-2 px-2 py-[5px] rounded-md text-xs text-ink2 cursor-pointer select-none"
              >
                <span className="inline-flex text-ink3"><ImportIcon /></span>
                import trace
              </div>
            </div>
          </div>

          <div className="flex-1" />

          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              diff selection
            </div>
            <DiffSelectionMini baseline={baseline} compare={compare} />
          </div>
        </aside>

        {/* main */}
        <div className="flex-1 overflow-x-hidden overflow-y-auto flex flex-col">

          {/* page header */}
          <div className="pt-[22px] px-6">
            <div className="font-mono text-[10px] text-ink3 uppercase tracking-[0.18em] mb-1.5">
              spaniel sessions
            </div>
            <div className="flex items-baseline gap-3.5">
              <h1 className="m-0 font-serif text-[28px] font-semibold tracking-[-0.02em] text-ink">
                Sessions
              </h1>
              <span className="font-sans text-[13px] text-ink2">
                Named windows of telemetry. Each branch usually gets its own.
              </span>
            </div>
          </div>

          {/* diff workflow explainer */}
          <div className="px-6 pt-[18px] pb-1">
            <HowToDiff />
          </div>

          {/* sessions table */}
          <div className="px-6 py-1">
            {/* column header */}
            <div
              className="grid gap-2.5 px-3.5 py-2 font-mono text-[9px] text-ink3 uppercase tracking-[0.14em] bg-surface2 rounded-t-[10px] border border-line"
              style={{ gridTemplateColumns: '34px minmax(0,1fr) 100px 64px 64px 168px' }}
            >
              <div title="Mark baseline">★</div>
              <div>session · created</div>
              <div>traces</div>
              <div>spans</div>
              <div className="col-span-2 text-right">actions</div>
            </div>

            {loading ? (
              <div className="px-4 py-6 font-mono text-xs text-ink3 bg-surface border border-line border-t-0 rounded-b-[10px]">
                loading…
              </div>
            ) : filteredSessions.length === 0 ? (
              sessions.length === 0 ? (
                <EmptyState
                  title="No sessions yet"
                  hint={<>Run <code>spaniel session new &lt;label&gt;</code> or call <code>POST /api/sessions</code> to create one.</>}
                  glyph={
                    <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                      <rect x="5" y="8" width="22" height="16" rx="3" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                      <line x1="5" y1="13" x2="27" y2="13" stroke="currentColor" strokeWidth="1" opacity="0.3" />
                      <circle cx="9" cy="10.5" r="1" fill="currentColor" opacity="0.5" />
                      <circle cx="12.5" cy="10.5" r="1" fill="currentColor" opacity="0.4" />
                      <circle cx="16" cy="10.5" r="1" fill="currentColor" opacity="0.3" />
                    </svg>
                  }
                />
              ) : (
                <div className="px-4 py-8 text-center font-mono text-xs text-ink3 bg-surface border border-line border-t-0 rounded-b-[10px]">
                  no sessions match this filter
                </div>
              )
            ) : (
              <div
                data-testid="sessions-table-body"
                className="bg-surface border border-line border-t-0 rounded-b-[10px] overflow-hidden"
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
            <div className="px-6 pt-[18px] pb-1">
              <div className="flex items-baseline gap-2.5 mb-2">
                <h2 className="m-0 font-serif text-base font-semibold text-ink">Recent diffs</h2>
                <span className="font-sans text-[11px] text-ink2">
                  cached locally · re-open any to compare again
                </span>
              </div>
              <div className="bg-surface border border-line rounded-[10px] overflow-hidden">
                {diffHistory.map((d, i) => (
                  <div
                    key={i}
                    className="grid gap-3 px-4 py-2.5 items-center"
                    style={{
                      gridTemplateColumns: 'minmax(0,1fr) 80px 80px 80px',
                      borderBottom: i < diffHistory.length - 1 ? '1px solid var(--line2)' : 'none',
                    }}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="font-mono text-[11.5px] text-ink2">
                        {d.baselineLabel}
                      </span>
                      <span className="text-ink3">→</span>
                      <span className="font-mono text-[11.5px] font-semibold text-ink">
                        {d.compareLabel}
                      </span>
                      <a
                        href={`/diff?baseline=${d.baselineId}&compare=${d.compareId}`}
                        className="font-mono text-[10px] text-accent-ink ml-1"
                      >
                        re-open
                      </a>
                    </div>
                    {d.deltaMs !== undefined ? (
                      <span
                        className="font-mono text-xs font-semibold"
                        style={{ color: d.deltaMs < 0 ? '#3e6a3e' : d.deltaMs > 0 ? 'var(--danger-ink)' : 'var(--ink3)' }}
                      >
                        {fmtDeltaMs(d.deltaMs * 1_000_000)}
                      </span>
                    ) : <span />}
                    {d.deltaSpans !== undefined ? (
                      <span
                        className="font-mono text-xs font-semibold"
                        style={{ color: d.deltaSpans < 0 ? '#3e6a3e' : d.deltaSpans > 0 ? 'var(--danger-ink)' : 'var(--ink3)' }}
                      >
                        {(d.deltaSpans > 0 ? '+' : '') + d.deltaSpans} spans
                      </span>
                    ) : <span />}
                    <span className="font-mono text-[10.5px] text-ink3">
                      {fmtRelativeMs(d.at)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="h-20" />
        </div>
      </div>

      {/* sticky compare bar */}
      <CompareBar baseline={baseline} compare={compare} />
    </div>
  )
}
