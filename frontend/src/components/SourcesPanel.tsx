import { useEffect, useState } from 'react'
import { api, SourceStats } from '@/lib/api'

function fmtRate(n: number): string {
  if (n <= 0) return '0'
  if (n < 1) return '<1'
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return n.toFixed(1)
}

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

function fmtPct(n: number): string {
  return `${(n * 100).toFixed(1)}%`
}

interface Props {
  onClose: () => void
}

export default function SourcesPanel({ onClose }: Props) {
  const [sources, setSources] = useState<SourceStats[]>([])
  const [sortKey, setSortKey] = useState<keyof SourceStats>('accepted_per_sec')

  useEffect(() => {
    let cancel = false
    function refresh() {
      api.sources.list()
        .then(r => { if (!cancel) setSources(r.data) })
        .catch(() => {})
    }
    refresh()
    const t = setInterval(refresh, 2000)
    return () => { cancel = true; clearInterval(t) }
  }, [])

  const sorted = [...sources].sort((a, b) => {
    const av = a[sortKey] as number
    const bv = b[sortKey] as number
    return bv - av
  }).slice(0, 10)

  function Col({ label, k }: { label: string; k: keyof SourceStats }) {
    return (
      <th
        className={`px-2 py-1 text-right cursor-pointer select-none ${sortKey === k ? 'text-ink' : 'text-ink3'}`}
        onClick={() => setSortKey(k)}
      >
        {label}
      </th>
    )
  }

  return (
    <div className="absolute bottom-7 right-0 w-[580px] rounded-lg border border-border bg-surface shadow-lg z-50 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 border-b border-border">
        <span className="font-mono text-[11px] text-ink font-medium tracking-wide">Sources</span>
        <button
          onClick={onClose}
          className="text-ink3 hover:text-ink text-[11px] leading-none px-1"
          aria-label="Close sources panel"
        >
          ✕
        </button>
      </div>

      {sorted.length === 0 ? (
        <p className="px-3 py-4 font-mono text-[10px] text-ink3">No sources seen yet.</p>
      ) : (
        <table className="w-full font-mono text-[10px]">
          <thead>
            <tr className="border-b border-border bg-surface-raised text-ink3">
              <th className="px-2 py-1 text-left">service</th>
              <Col label="acc/s"  k="accepted_per_sec" />
              <Col label="rej/s"  k="rejected_per_sec" />
              <Col label="errors" k="error_rate" />
              <Col label="bytes/s" k="bytes_per_sec" />
            </tr>
          </thead>
          <tbody>
            {sorted.map(s => {
              const rateLimited = s.rejected_per_sec > 0
              return (
                <tr key={s.service} className="border-b border-border/50 hover:bg-surface-raised/40">
                  <td className="px-2 py-[3px] text-ink flex items-center gap-1">
                    {rateLimited && (
                      <span
                        className="inline-block w-[6px] h-[6px] rounded-full bg-danger shrink-0"
                        title="Rate limited"
                        aria-label="rate-limited"
                      />
                    )}
                    <span className="truncate max-w-[180px]">{s.service}</span>
                  </td>
                  <td className="px-2 py-[3px] text-right text-ok">{fmtRate(s.accepted_per_sec)}</td>
                  <td className={`px-2 py-[3px] text-right ${rateLimited ? 'text-danger' : 'text-ink3'}`}>
                    {fmtRate(s.rejected_per_sec)}
                  </td>
                  <td className={`px-2 py-[3px] text-right ${s.error_rate > 0.05 ? 'text-warning' : 'text-ink3'}`}>
                    {fmtPct(s.error_rate)}
                  </td>
                  <td className="px-2 py-[3px] text-right text-ink2">{fmtBytes(s.bytes_per_sec)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}
