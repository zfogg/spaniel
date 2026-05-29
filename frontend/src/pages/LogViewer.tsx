import { useEffect, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { qk } from '@/lib/query'
import { useNavigate } from 'react-router-dom'
import { api, Log } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import EmptyState from '@/components/EmptyState'
import JsonView from '@/components/JsonView'

// ── severity helpers ──────────────────────────────────────────────────────────

export function sevLabel(n: number): string {
  if (n >= 21) return 'FATAL'
  if (n >= 17) return 'ERROR'
  if (n >= 13) return 'WARN'
  if (n >= 9)  return 'INFO'
  if (n >= 5)  return 'DEBUG'
  return 'TRACE'
}

function sevBadgeStyle(n: number): React.CSSProperties {
  const base: React.CSSProperties = {
    borderRadius: 3,
    padding: '1px 5px',
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.06em',
    textTransform: 'uppercase',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
    display: 'inline-block',
  }
  if (n >= 21) return { ...base, color: '#fff',         background: 'var(--danger)' }
  if (n >= 17) return { ...base, color: 'var(--danger)', background: 'var(--danger-bg)' }
  if (n >= 13) return { ...base, color: 'var(--warn)',   background: 'var(--warn-bg)' }
  if (n >= 9)  return { ...base, color: 'var(--ink2)',   background: 'transparent' }
  if (n >= 5)  return { ...base, color: 'var(--ink3)',   background: 'transparent' }
  return       { ...base, color: 'var(--ink3)', background: 'transparent', opacity: 0.7 }
}

function sevBodyColor(n: number): string {
  if (n >= 13) return 'var(--ink)'
  if (n >= 9)  return 'var(--ink2)'
  return 'var(--ink3)'
}

function isZeroTraceId(id: string): boolean {
  return !id || /^0+$/.test(id)
}

// ── timestamp helpers ─────────────────────────────────────────────────────────

function fmtAbsolute(ns: number): string {
  const d = new Date(ns / 1_000_000)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}

function fmtRelative(ns: number, nowMs: number): string {
  const diffMs = nowMs - ns / 1_000_000
  if (diffMs < 0) return 'just now'
  if (diffMs < 1000) return `${Math.round(diffMs)}ms ago`
  if (diffMs < 60_000) return `${Math.round(diffMs / 1000)}s ago`
  if (diffMs < 3_600_000) return `${Math.round(diffMs / 60_000)}m ago`
  return `${Math.round(diffMs / 3_600_000)}h ago`
}

// ── severity filter chip levels ───────────────────────────────────────────────

type SevFilter = 'ALL' | 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

const SEV_ORDER: SevFilter[] = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']

function chipActiveStyle(chip: SevFilter): React.CSSProperties {
  const base: React.CSSProperties = {
    padding: '3px 10px',
    borderRadius: 12,
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.05em',
    textTransform: 'uppercase',
    cursor: 'pointer',
    border: '1px solid transparent',
    background: 'transparent',
  }
  switch (chip) {
    case 'FATAL': return { ...base, color: '#fff',         background: 'var(--danger)',    border: '1px solid var(--danger)' }
    case 'ERROR': return { ...base, color: 'var(--danger)', background: 'var(--danger-bg)', border: '1px solid var(--danger)' }
    case 'WARN':  return { ...base, color: 'var(--warn)',   background: 'var(--warn-bg)',   border: '1px solid var(--warn)' }
    case 'INFO':  return { ...base, color: 'var(--accent)', background: 'var(--accent-bg)', border: '1px solid var(--accent)' }
    case 'DEBUG': return { ...base, color: 'var(--ink3)',   background: 'var(--surface3)', border: '1px solid var(--line)' }
    case 'TRACE': return { ...base, color: 'var(--ink3)',   background: 'var(--surface3)', border: '1px solid var(--line)', opacity: 0.85 }
    default:      return { ...base, color: 'var(--ink)',    background: 'var(--surface2)', border: '1px solid var(--line)' }
  }
}

function chipInactiveStyle(): React.CSSProperties {
  return {
    padding: '3px 10px',
    borderRadius: 12,
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: '0.05em',
    textTransform: 'uppercase',
    cursor: 'pointer',
    border: '1px solid transparent',
    background: 'transparent',
    color: 'var(--ink3)',
  }
}

export function matchesSevFilter(severity: number, filter: SevFilter): boolean {
  if (filter === 'ALL')   return true
  if (filter === 'FATAL') return severity >= 21
  if (filter === 'ERROR') return severity >= 17 && severity < 21
  if (filter === 'WARN')  return severity >= 13 && severity < 17
  if (filter === 'INFO')  return severity >= 9  && severity < 13
  if (filter === 'DEBUG') return severity >= 5  && severity < 9
  if (filter === 'TRACE') return severity < 5
  return true
}

function presentSevFilters(logs: Log[]): SevFilter[] {
  const present = new Set<SevFilter>()
  for (const l of logs) present.add(sevLabel(l.severity) as SevFilter)
  return SEV_ORDER.filter(s => present.has(s))
}

// ── LogRow ────────────────────────────────────────────────────────────────────

function LogRow({ log, i, nowMs, selected, onSelect, navigate }: {
  log: Log
  i: number
  nowMs: number
  selected: boolean
  onSelect: () => void
  navigate: (path: string) => void
}) {
  const [hovered, setHovered] = useState(false)
  const c = svcColor(log.service_name)

  const rowBg = selected
    ? `color-mix(in oklch, var(--accent) 14%, var(--surface))`
    : hovered
      ? 'var(--surface2)'
      : i % 2 === 0 ? 'var(--surface)' : 'var(--bg)'

  const hasTrace = log.trace_id && !isZeroTraceId(log.trace_id)

  return (
    <div
      role="button"
      tabIndex={0}
      aria-selected={selected}
      onClick={onSelect}
      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect() } }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      className={`grid items-center h-7 border-b border-line2 px-2 gap-1.5 cursor-pointer transition-[background] duration-[70ms] ${selected ? 'border-l-2 border-l-accent-d' : 'border-l-2 border-l-transparent'}`}
      style={{
        gridTemplateColumns: '90px 52px 130px 1fr 22px',
        background: rowBg,
      }}
    >
      {/* timestamp */}
      <div
        title={fmtAbsolute(log.timestamp_ns)}
        className="font-mono text-[11px] text-ink3 overflow-hidden text-ellipsis whitespace-nowrap shrink-0"
      >
        {fmtRelative(log.timestamp_ns, nowMs)}
      </div>

      {/* severity badge */}
      <div className="flex items-center">
        <span style={sevBadgeStyle(log.severity)}>{sevLabel(log.severity)}</span>
      </div>

      {/* service chip */}
      <div className="flex items-center gap-[5px] overflow-hidden min-w-0">
        <span
          className="w-[5px] h-[5px] rounded-full shrink-0"
          style={{ background: c.fg }}
        />
        <span className="font-mono text-[10.5px] text-ink3 overflow-hidden text-ellipsis whitespace-nowrap">
          {log.service_name}
        </span>
      </div>

      {/* body */}
      <div
        className="font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap"
        style={{ color: sevBodyColor(log.severity) }}
      >
        {log.body}
      </div>

      {/* trace link */}
      <div className="flex items-center justify-center">
        {hasTrace && (
          <button
            type="button"
            onClick={e => {
              e.stopPropagation()
              const q = log.span_id && !/^0+$/.test(log.span_id) ? `?spanId=${log.span_id}` : ''
              navigate(`/traces/${log.trace_id}${q}`)
            }}
            title={`view trace ${log.trace_id}`}
            className="bg-none border-none cursor-pointer py-0.5 px-[3px] rounded-[3px] font-mono text-[9px] text-accent-d leading-none flex items-center"
          >
            →
          </button>
        )}
      </div>
    </div>
  )
}

// ── LogInspector (right panel) ────────────────────────────────────────────────

function LogInspector({ log, onClose, navigate }: {
  log: Log
  onClose: () => void
  navigate: (path: string) => void
}) {
  const c = svcColor(log.service_name)
  const hasTrace = log.trace_id && !isZeroTraceId(log.trace_id)

  let attrs: Record<string, unknown> = {}
  try { attrs = JSON.parse(log.attributes || '{}') } catch { /* empty */ }
  const attrEntries = Object.entries(attrs)

  function openTrace() {
    if (!hasTrace) return
    const q = log.span_id && !/^0+$/.test(log.span_id) ? `?spanId=${log.span_id}` : ''
    navigate(`/traces/${log.trace_id}${q}`)
  }

  return (
    <aside
      aria-label="Log details"
      className="w-[360px] shrink-0 border-l border-line bg-surface flex flex-col overflow-hidden"
    >
      <header className="flex items-center gap-2 px-[14px] py-2.5 border-b border-line shrink-0">
        <span style={sevBadgeStyle(log.severity)}>{sevLabel(log.severity)}</span>
        <span className="font-mono text-[11px] text-ink2">
          {fmtAbsolute(log.timestamp_ns)}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          onClick={onClose}
          aria-label="Close inspector"
          className="bg-none border-none cursor-pointer text-ink3 text-sm leading-none px-1.5 py-0.5"
        >
          ×
        </button>
      </header>

      <div className="flex-1 overflow-y-auto px-[14px] py-3">
        {/* service chip */}
        <div className="flex items-center gap-1.5 mb-2.5">
          <span className="w-[7px] h-[7px] rounded-full shrink-0" style={{ background: c.fg }} />
          <span className="font-mono text-[11px] text-ink2">
            {log.service_name || '—'}
          </span>
        </div>

        {/* body (full, wrapped) */}
        <div
          className="font-mono text-[11.5px] leading-[1.5] whitespace-pre-wrap break-words bg-surface2 border border-line rounded-[5px] px-2.5 py-2 mb-3.5"
          style={{ color: sevBodyColor(log.severity) }}
        >
          {log.body || '(empty)'}
        </div>

        {/* attributes */}
        <div className="font-mono text-[9px] text-ink3 tracking-[0.1em] uppercase mb-1.5">
          attributes
        </div>
        {attrEntries.length === 0 ? (
          <div className="font-mono text-[11px] text-ink3">
            none
          </div>
        ) : (
          <JsonView data={attrs} />
        )}

        {/* trace + span IDs */}
        {(hasTrace || log.span_id) && (
          <div className="mt-3.5">
            <div className="font-mono text-[9px] text-ink3 tracking-[0.1em] uppercase mb-1.5">
              ids
            </div>
            <div
              className="grid gap-y-[3px] gap-x-2.5 font-mono text-[11px]"
              style={{ gridTemplateColumns: 'auto 1fr' }}
            >
              {hasTrace && (<><span className="text-ink3">trace</span><span className="text-ink2 break-all">{log.trace_id}</span></>)}
              {log.span_id && !/^0+$/.test(log.span_id) && (<><span className="text-ink3">span</span><span className="text-ink2 break-all">{log.span_id}</span></>)}
            </div>
          </div>
        )}
      </div>

      {hasTrace && (
        <footer className="px-[14px] py-2.5 border-t border-line shrink-0 bg-surface">
          <button
            type="button"
            onClick={openTrace}
            className="w-full px-3 py-[7px] rounded-[5px] border border-accent-d bg-accent-bg text-accent-ink font-mono text-[11px] font-semibold cursor-pointer"
          >
            Open trace →
          </button>
        </footer>
      )}
    </aside>
  )
}


// ── LogViewer ─────────────────────────────────────────────────────────────────

export default function LogViewer() {
  const [filterService, setFilterService] = useState('all')
  const [filterSev, setFilterSev]     = useState<SevFilter>('ALL')
  const [search, setSearch]           = useState('')
  const [nowMs, setNowMs]             = useState(() => Date.now())
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const navigate = useNavigate()

  // Logs + services come from queries; live log events refresh them via
  // useLiveInvalidation() in App.tsx (throttled), replacing the old initial
  // load + 3s poll + WebSocket-push + client-side dedup machinery.
  const { data: logs = [], isLoading: loading } = useQuery({
    queryKey: qk.logs({}),
    queryFn: () => api.logs.list({}).then(r => r.data ?? []),
  })
  const { data: services = [] } = useQuery({
    queryKey: qk.services(),
    queryFn: () => api.services.list().then(r => r.data ?? []),
  })

  // tick relative timestamps every second
  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1_000)
    return () => clearInterval(id)
  }, [])

  // Esc closes the inspector.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setSelectedKey(null) }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const filtered = logs.filter(l => {
    if (filterService !== 'all' && l.service_name !== filterService) return false
    if (!matchesSevFilter(l.severity, filterSev)) return false
    if (search) {
      const q = search.toLowerCase()
      if (!l.body.toLowerCase().includes(q) && !l.service_name.toLowerCase().includes(q)) return false
    }
    return true
  })

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* filter bar */}
      <div className="flex items-center gap-2.5 px-[14px] py-2 bg-surface border-b border-line shrink-0 flex-wrap">
        {/* search */}
        <input
          type="text"
          placeholder="search logs…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          data-shortcut="search"
          className="w-[260px] h-7 bg-surface2 border border-line rounded-[5px] px-2.5 font-mono text-[11px] text-ink outline-none"
        />

        {/* service filter */}
        <select
          value={filterService}
          onChange={e => setFilterService(e.target.value)}
          className="h-7 bg-surface2 border border-line rounded-[5px] px-2 font-mono text-[11px] text-ink cursor-pointer outline-none"
        >
          <option value="all">all services</option>
          {services.map(s => <option key={s} value={s}>{s}</option>)}
        </select>

        {/* severity chips — ALL + one per present severity */}
        <div className="flex items-center gap-1">
          {(['ALL', ...presentSevFilters(logs)] as SevFilter[]).map(chip => (
            <button
              key={chip}
              type="button"
              onClick={() => setFilterSev(chip)}
              style={filterSev === chip ? chipActiveStyle(chip) : chipInactiveStyle()}
            >
              {chip}
            </button>
          ))}
        </div>

        <div className="flex-1" />

        {/* log count */}
        <span className="font-mono text-[10px] text-ink3">
          {filtered.length} logs
        </span>
      </div>

      {/* body (list + optional right inspector) */}
      <div className="flex-1 flex min-h-0">
        <div className="flex-1 flex flex-col min-w-0">
          {loading ? (
            <div className="flex items-center justify-center flex-1 font-mono text-xs text-ink3">
              loading…
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              title="No logs yet"
              hint={<>Point your OTLP exporter at <code>localhost:4318/v1/logs</code> and logs will stream in here.</>}
              glyph={
                <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                  <rect x="6" y="6" width="20" height="20" rx="3" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                  <line x1="10" y1="12" x2="22" y2="12" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                  <line x1="10" y1="16" x2="20" y2="16" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
                  <line x1="10" y1="20" x2="17" y2="20" stroke="currentColor" strokeWidth="1.5" opacity="0.3" />
                </svg>
              }
            />
          ) : (
            <>
              {/* column header */}
              <div
                className="grid items-center h-6 pl-2.5 pr-2 gap-1.5 bg-surface border-b border-line shrink-0"
                style={{ gridTemplateColumns: '90px 52px 130px 1fr 22px' }}
              >
                {['time', 'level', 'service', 'body', ''].map((h, i) => (
                  <div key={i} className="font-mono text-[9px] uppercase tracking-[0.12em] text-ink3">
                    {h}
                  </div>
                ))}
              </div>

              {/* rows */}
              <div className="flex-1 overflow-y-auto overflow-x-hidden">
                {filtered.map((log, i) => {
                  const k = logKey(log)
                  return (
                    <LogRow
                      key={k}
                      log={log}
                      i={i}
                      nowMs={nowMs}
                      selected={selectedKey === k}
                      onSelect={() => setSelectedKey(prev => prev === k ? null : k)}
                      navigate={navigate}
                    />
                  )
                })}
              </div>
            </>
          )}
        </div>

        {selectedKey && (() => {
          const sel = filtered.find(l => logKey(l) === selectedKey)
          return sel ? (
            <LogInspector log={sel} onClose={() => setSelectedKey(null)} navigate={navigate} />
          ) : null
        })()}
      </div>
    </div>
  )
}

function logKey(l: Log): string {
  return `${l.timestamp_ns}:${l.span_id}:${l.body.slice(0, 40)}`
}
