import { useEffect, useState, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, Log } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import { useWS } from '@/lib/ws'

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
      style={{
        display: 'grid',
        gridTemplateColumns: '90px 52px 130px 1fr 22px',
        alignItems: 'center',
        height: 28,
        background: rowBg,
        borderBottom: '1px solid var(--line2)',
        borderLeft: selected ? '2px solid var(--accent)' : '2px solid transparent',
        paddingLeft: 8,
        paddingRight: 8,
        gap: 6,
        transition: 'background 0.07s',
        cursor: 'pointer',
      }}
    >
      {/* timestamp */}
      <div
        title={fmtAbsolute(log.timestamp_ns)}
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          color: 'var(--ink3)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flexShrink: 0,
        }}
      >
        {fmtRelative(log.timestamp_ns, nowMs)}
      </div>

      {/* severity badge */}
      <div style={{ display: 'flex', alignItems: 'center' }}>
        <span style={sevBadgeStyle(log.severity)}>{sevLabel(log.severity)}</span>
      </div>

      {/* service chip */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 5, overflow: 'hidden', minWidth: 0 }}>
        <span style={{
          width: 5, height: 5,
          borderRadius: '50%',
          background: c.fg,
          flexShrink: 0,
        }} />
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 10.5,
          color: 'var(--ink3)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}>
          {log.service_name}
        </span>
      </div>

      {/* body */}
      <div style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 11,
        color: sevBodyColor(log.severity),
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}>
        {log.body}
      </div>

      {/* trace link */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {hasTrace && (
          <button
            type="button"
            onClick={e => {
              e.stopPropagation()
              const q = log.span_id && !/^0+$/.test(log.span_id) ? `?spanId=${log.span_id}` : ''
              navigate(`/traces/${log.trace_id}${q}`)
            }}
            title={`view trace ${log.trace_id}`}
            style={{
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              padding: '2px 3px',
              borderRadius: 3,
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: 'var(--accent)',
              lineHeight: 1,
              display: 'flex',
              alignItems: 'center',
            }}
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
      style={{
        width: 360,
        flexShrink: 0,
        borderLeft: '1px solid var(--border)',
        background: 'var(--surface)',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
    >
      <header style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '10px 14px',
        borderBottom: '1px solid var(--line)',
        flexShrink: 0,
      }}>
        <span style={sevBadgeStyle(log.severity)}>{sevLabel(log.severity)}</span>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink2)' }}>
          {fmtAbsolute(log.timestamp_ns)}
        </span>
        <div style={{ flex: 1 }} />
        <button
          type="button"
          onClick={onClose}
          aria-label="Close inspector"
          style={{
            background: 'none', border: 'none', cursor: 'pointer',
            color: 'var(--ink3)', fontSize: 14, lineHeight: 1, padding: '2px 6px',
          }}
        >
          ×
        </button>
      </header>

      <div style={{ flex: 1, overflowY: 'auto', padding: '12px 14px' }}>
        {/* service chip */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10 }}>
          <span style={{ width: 7, height: 7, borderRadius: '50%', background: c.fg, flexShrink: 0 }} />
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink2)' }}>
            {log.service_name || '—'}
          </span>
        </div>

        {/* body (full, wrapped) */}
        <div style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 11.5,
          lineHeight: 1.5,
          color: sevBodyColor(log.severity),
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          background: 'var(--surface2)',
          border: '1px solid var(--line)',
          borderRadius: 5,
          padding: '8px 10px',
          marginBottom: 14,
        }}>
          {log.body || '(empty)'}
        </div>

        {/* attributes */}
        <div style={{
          fontFamily: 'var(--font-mono)', fontSize: 9,
          color: 'var(--ink3)', letterSpacing: '0.1em', textTransform: 'uppercase',
          marginBottom: 6,
        }}>
          attributes
        </div>
        {attrEntries.length === 0 ? (
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink3)' }}>
            none
          </div>
        ) : (
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'auto 1fr',
            gap: '3px 10px',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
          }}>
            {attrEntries.map(([k, v]) => (
              <Attribute key={k} k={k} v={v} />
            ))}
          </div>
        )}

        {/* trace + span IDs */}
        {(hasTrace || log.span_id) && (
          <div style={{ marginTop: 14 }}>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9,
              color: 'var(--ink3)', letterSpacing: '0.1em', textTransform: 'uppercase',
              marginBottom: 6,
            }}>
              ids
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '3px 10px',
              fontFamily: 'var(--font-mono)', fontSize: 11 }}>
              {hasTrace && (<><span style={{ color: 'var(--ink3)' }}>trace</span><span style={{ color: 'var(--ink2)', wordBreak: 'break-all' }}>{log.trace_id}</span></>)}
              {log.span_id && !/^0+$/.test(log.span_id) && (<><span style={{ color: 'var(--ink3)' }}>span</span><span style={{ color: 'var(--ink2)', wordBreak: 'break-all' }}>{log.span_id}</span></>)}
            </div>
          </div>
        )}
      </div>

      {hasTrace && (
        <footer style={{
          padding: '10px 14px',
          borderTop: '1px solid var(--line)',
          flexShrink: 0,
          background: 'var(--surface)',
        }}>
          <button
            type="button"
            onClick={openTrace}
            style={{
              width: '100%',
              padding: '7px 12px',
              borderRadius: 5,
              border: '1px solid var(--accent)',
              background: 'var(--accent-bg)',
              color: 'var(--accent-ink)',
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Open trace →
          </button>
        </footer>
      )}
    </aside>
  )
}

function Attribute({ k, v }: { k: string; v: unknown }) {
  const text = typeof v === 'string' ? v : JSON.stringify(v)
  return (
    <>
      <span style={{ color: 'var(--ink3)', whiteSpace: 'nowrap' }}>{k}</span>
      <span style={{ color: 'var(--ink2)', wordBreak: 'break-word' }}>{text}</span>
    </>
  )
}

// ── LogViewer ─────────────────────────────────────────────────────────────────

export default function LogViewer() {
  const [logs, setLogs]               = useState<Log[]>([])
  const [services, setServices]       = useState<string[]>([])
  const [filterService, setFilterService] = useState('all')
  const [filterSev, setFilterSev]     = useState<SevFilter>('ALL')
  const [search, setSearch]           = useState('')
  const [loading, setLoading]         = useState(true)
  const [nowMs, setNowMs]             = useState(() => Date.now())
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const navigate = useNavigate()
  const knownIds = useRef<Set<string>>(new Set())

  // initial load + services
  useEffect(() => {
    api.logs.list({}).then(r => {
      const rows = r.data ?? []
      setLogs(rows)
      for (const l of rows) knownIds.current.add(logKey(l))
      setLoading(false)
    }).catch(() => setLoading(false))
    api.services.list().then(r => setServices(r.data ?? []))
  }, [])

  // live poll every 3s
  useEffect(() => {
    const id = setInterval(() => {
      api.logs.list({}).then(r => {
        const rows = r.data ?? []
        const newRows = rows.filter(l => !knownIds.current.has(logKey(l)))
        if (newRows.length > 0) {
          for (const l of newRows) knownIds.current.add(logKey(l))
          setLogs(prev => [...newRows, ...prev].slice(0, 500))
        }
      }).catch(() => { /* ignore poll errors */ })
    }, 3_000)
    return () => clearInterval(id)
  }, [])

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

  // Live log push via WebSocket
  useWS(ev => {
    if (ev.type !== 'log') return
    const p = ev.payload
    const l: Log = {
      timestamp_ns: ev.timestamp_ns,
      trace_id:     p.traceId,
      span_id:      p.spanId,
      severity:     p.severity,
      body:         p.body,
      attributes:   '{}',
      service_name: p.serviceName,
      session_id:   p.sessionId,
      received_at:  ev.timestamp_ns,
    }
    const key = logKey(l)
    if (knownIds.current.has(key)) return
    knownIds.current.add(key)
    setLogs(prev => [l, ...prev].slice(0, 500))
  })

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
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* filter bar */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        padding: '8px 14px',
        background: 'var(--surface)',
        borderBottom: '1px solid var(--line)',
        flexShrink: 0,
        flexWrap: 'wrap',
      }}>
        {/* search */}
        <input
          type="text"
          placeholder="search logs…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          data-shortcut="search"
          style={{
            width: 260,
            height: 28,
            background: 'var(--surface2)',
            border: '1px solid var(--line)',
            borderRadius: 5,
            padding: '0 10px',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--ink)',
            outline: 'none',
          }}
        />

        {/* service filter */}
        <select
          value={filterService}
          onChange={e => setFilterService(e.target.value)}
          style={{
            height: 28,
            background: 'var(--surface2)',
            border: '1px solid var(--line)',
            borderRadius: 5,
            padding: '0 8px',
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--ink)',
            cursor: 'pointer',
            outline: 'none',
          }}
        >
          <option value="all">all services</option>
          {services.map(s => <option key={s} value={s}>{s}</option>)}
        </select>

        {/* severity chips — ALL + one per present severity */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
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

        <div style={{ flex: 1 }} />

        {/* log count */}
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 10,
          color: 'var(--ink3)',
        }}>
          {filtered.length} logs
        </span>
      </div>

      {/* body (list + optional right inspector) */}
      <div style={{ flex: 1, display: 'flex', minHeight: 0 }}>
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          {loading ? (
            <div style={{
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--ink3)',
            }}>
              loading…
            </div>
          ) : filtered.length === 0 ? (
            <div style={{
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--ink3)',
              textAlign: 'center', padding: 24,
            }}>
              no logs yet — send traces with OTel logging attached
            </div>
          ) : (
            <>
              {/* column header */}
              <div style={{
                display: 'grid',
                gridTemplateColumns: '90px 52px 130px 1fr 22px',
                alignItems: 'center',
                height: 24,
                paddingLeft: 10,
                paddingRight: 8,
                gap: 6,
                background: 'var(--surface)',
                borderBottom: '1px solid var(--line)',
                flexShrink: 0,
              }}>
                {['time', 'level', 'service', 'body', ''].map((h, i) => (
                  <div key={i} style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 9,
                    textTransform: 'uppercase',
                    letterSpacing: '0.12em',
                    color: 'var(--ink3)',
                  }}>
                    {h}
                  </div>
                ))}
              </div>

              {/* rows */}
              <div style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden' }}>
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
