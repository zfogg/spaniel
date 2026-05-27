import { useEffect, useState, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, Log } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'

// ── severity helpers ──────────────────────────────────────────────────────────

function sevLabel(n: number): string {
  if (n >= 17) return 'ERROR'
  if (n >= 13) return 'WARN'
  if (n >= 9)  return 'INFO'
  return 'DEBUG'
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
  if (n >= 17) return { ...base, color: 'var(--danger)', background: 'var(--danger-bg)' }
  if (n >= 13) return { ...base, color: 'var(--warn)', background: 'var(--warn-bg)' }
  if (n >= 9)  return { ...base, color: 'var(--ink2)', background: 'transparent' }
  return { ...base, color: 'var(--ink3)', background: 'transparent' }
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

type SevFilter = 'ALL' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'

const SEV_CHIPS: SevFilter[] = ['ALL', 'DEBUG', 'INFO', 'WARN', 'ERROR']

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
    case 'ERROR': return { ...base, color: 'var(--danger)', background: 'var(--danger-bg)', border: '1px solid var(--danger)' }
    case 'WARN':  return { ...base, color: 'var(--warn)',   background: 'var(--warn-bg)',   border: '1px solid var(--warn)' }
    case 'INFO':  return { ...base, color: 'var(--accent)', background: 'var(--accent-bg)', border: '1px solid var(--accent)' }
    case 'DEBUG': return { ...base, color: 'var(--ink3)',   background: 'var(--surface3)', border: '1px solid var(--line)' }
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

function matchesSevFilter(severity: number, filter: SevFilter): boolean {
  if (filter === 'ALL')   return true
  if (filter === 'ERROR') return severity >= 17
  if (filter === 'WARN')  return severity >= 13 && severity < 17
  if (filter === 'INFO')  return severity >= 9  && severity < 13
  if (filter === 'DEBUG') return severity < 9
  return true
}

// ── LogRow ────────────────────────────────────────────────────────────────────

function LogRow({ log, i, nowMs, navigate }: {
  log: Log
  i: number
  nowMs: number
  navigate: (path: string) => void
}) {
  const [hovered, setHovered] = useState(false)
  const c = svcColor(log.service_name)

  const rowBg = hovered
    ? 'var(--surface2)'
    : i % 2 === 0 ? 'var(--surface)' : 'var(--bg)'

  const hasTrace = log.trace_id && !isZeroTraceId(log.trace_id)

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        display: 'grid',
        gridTemplateColumns: '90px 52px 130px 1fr 22px',
        alignItems: 'center',
        height: 28,
        background: rowBg,
        borderBottom: '1px solid var(--line2)',
        paddingLeft: 10,
        paddingRight: 8,
        gap: 6,
        transition: 'background 0.07s',
        cursor: 'default',
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
            onClick={() => navigate('/traces/' + log.trace_id)}
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

// ── LogViewer ─────────────────────────────────────────────────────────────────

export default function LogViewer() {
  const [logs, setLogs]               = useState<Log[]>([])
  const [services, setServices]       = useState<string[]>([])
  const [filterService, setFilterService] = useState('all')
  const [filterSev, setFilterSev]     = useState<SevFilter>('ALL')
  const [search, setSearch]           = useState('')
  const [loading, setLoading]         = useState(true)
  const [nowMs, setNowMs]             = useState(() => Date.now())
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

        {/* severity chips */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          {SEV_CHIPS.map(chip => (
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

      {/* body */}
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
            {filtered.map((log, i) => (
              <LogRow
                key={logKey(log)}
                log={log}
                i={i}
                nowMs={nowMs}
                navigate={navigate}
              />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function logKey(l: Log): string {
  return `${l.timestamp_ns}:${l.span_id}:${l.body.slice(0, 40)}`
}
