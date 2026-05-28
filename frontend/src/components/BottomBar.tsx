import { useEffect, useState } from 'react'
import { api, Stats } from '@/lib/api'
import { useWSStatus, useWS } from '@/lib/ws'

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

interface ActiveSession { id: string; label: string }

export default function BottomBar() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [active, setActive] = useState<ActiveSession | null>(null)
  const [spansPerSec, setSpansPerSec] = useState(0)
  const connected = useWSStatus()

  useWS(ev => {
    if (ev.type !== 'throughput') return
    setSpansPerSec(ev.payload.spansPerSec)
  })

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

  return (
    <footer className="h-6 shrink-0 flex items-center gap-[14px] px-3 border-t border-border bg-surface font-mono text-[10px] text-ink2 tracking-[0.02em] select-none">
      <Stat label="db"     value={stats ? fmtBytes(stats.db_size) : '—'} />
      <Stat label="spans"  value={stats ? fmtCount(stats.span_count) : '—'} />
      <Stat label="traces" value={stats ? fmtCount(stats.trace_count) : '—'} />
      <Stat label="logs"   value={stats ? fmtCount(stats.log_count) : '—'} />
      {spansPerSec > 0 && <Stat label="rate" value={`${spansPerSec.toFixed(1)}/s`} />}

      <div className="flex-1" />

      {active?.id && (
        <Stat label="session" value={active.label || active.id.slice(0, 8)} />
      )}

      <div className="inline-flex items-center gap-[5px]">
        <span
          aria-hidden
          className={`w-[7px] h-[7px] rounded-full ${connected ? 'bg-ok' : 'bg-danger'}`}
          style={{
            boxShadow: connected
              ? '0 0 0 2px color-mix(in srgb, var(--ok) 25%, transparent)'
              : 'none',
          }}
        />
        <span className="text-ink2">
          {connected ? 'live' : 'disconnected'}
        </span>
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
