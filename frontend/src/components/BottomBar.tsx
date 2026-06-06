import { useEffect, useRef, useState } from 'react'
import { api, Stats } from '@/lib/api'
import { useWSStatus } from '@/lib/ws'
import SourcesPanel from './SourcesPanel'

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

function fmtCount(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

export function fmtRate(n: number): string {
  if (n <= 0) return '0 / s'
  if (n < 1) return '< 1 / s'
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k / s`
  return `${Math.round(n)} / s`
}

// fmtDuration renders an elapsed millisecond span compactly: 5s, 1m 5s, 1h 2m.
export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

interface ActiveSession { id: string; label: string }

export default function BottomBar() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [active, setActive] = useState<ActiveSession | null>(null)
  const [showSources, setShowSources] = useState(false)
  const footerRef = useRef<HTMLElement>(null)
  const { connected, since } = useWSStatus()
  const [now, setNow] = useState(() => Date.now())

  // Tick once a second so the "connected for <duration>" readout stays live.
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [])

  // Close sources panel on outside click.
  useEffect(() => {
    if (!showSources) return
    function onPointerDown(e: PointerEvent) {
      if (footerRef.current && !footerRef.current.contains(e.target as Node)) {
        setShowSources(false)
      }
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [showSources])

  useEffect(() => {
    let cancel = false
    function refresh() {
      api.stats.get().then(r => { if (!cancel) setStats(r.data) }).catch(() => {})
      api.sessions.getActive()
        .then(r => { if (!cancel) setActive(r.data) })
        .catch(() => { if (!cancel) setActive(null) })
    }
    refresh()
    const t = setInterval(refresh, 4000)
    return () => { cancel = true; clearInterval(t) }
  }, [])

  const spansPerSec = stats?.spans_per_sec ?? 0
  const live = spansPerSec > 0

  return (
    <footer ref={footerRef} className="relative h-6 shrink-0 flex items-center gap-[14px] px-3 border-t border-border bg-surface font-mono text-[10px] text-ink2 tracking-[0.02em] select-none">
      {showSources && <SourcesPanel onClose={() => setShowSources(false)} />}
      <Stat label="db"     value={stats ? fmtBytes(stats.db_size) : '—'} />
      <Stat label="spans"  value={stats ? fmtCount(stats.span_count) : '—'} />
      <Stat label="traces" value={stats ? fmtCount(stats.trace_count) : '—'} />
      <Stat label="logs"   value={stats ? fmtCount(stats.log_count) : '—'} />
      {live && <Stat label="spans/s" value={fmtRate(spansPerSec)} />}
      {(stats?.dropped_spans ?? 0) > 0 && (
        <span className="inline-flex items-center gap-[4px] px-[5px] h-[14px] rounded-sm bg-warning/15 text-warning font-mono text-[9px] tracking-[0.02em]">
          {fmtCount(stats!.dropped_spans)} dropped
        </span>
      )}

      <div className="flex-1" />

      {active?.id && (
        <button
          onClick={() => setShowSources(v => !v)}
          className="inline-flex items-baseline gap-[5px] hover:text-ink focus:outline-none"
          aria-label="Toggle sources panel"
          title="Sources — click to inspect per-service ingest rates"
        >
          <span className="text-ink3">session</span>
          <span className="text-ink">{active.label || active.id.slice(0, 8)}</span>
        </button>
      )}

      <div className="inline-flex items-center gap-[5px]">
        <span
          aria-hidden
          className={[
            'w-[7px] h-[7px] rounded-full',
            connected ? 'bg-ok' : 'bg-danger',
            live ? 'animate-pulse' : '',
          ].join(' ')}
          style={{
            boxShadow: connected
              ? '0 0 0 2px color-mix(in srgb, var(--ok) 25%, transparent)'
              : 'none',
          }}
        />
        <span className={connected ? 'text-ink2' : 'text-danger'}>
          {connected ? 'live' : 'disconnected'}
        </span>
        {connected && since != null && (
          <span className="text-ink3" title="Time connected to the backend">
            {fmtDuration(now - since)}
          </span>
        )}
      </div>
    </footer>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-baseline gap-[5px]">
      <span className="text-ink3">{label}</span>
      <span className="text-ink">{value}</span>
    </span>
  )
}
