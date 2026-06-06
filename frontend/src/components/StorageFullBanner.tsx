import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { api } from '@/lib/api'
import { qk } from '@/lib/query'

// StorageFullBanner shows a persistent alert across all pages whenever the
// backend reports storage_full (DB at its size cap / disk out of space). While
// full, the OTLP receivers return 503 to exporters. The banner auto-hides once
// space frees (stats.storage_full clears).
export default function StorageFullBanner() {
  const qc = useQueryClient()
  const [pruning, setPruning] = useState(false)

  const { data: stats } = useQuery({
    queryKey: qk.stats(),
    queryFn: () => api.stats.get().then((r) => r.data),
    refetchInterval: 5000,
  })

  if (!stats?.storage_full) return null

  async function prune() {
    setPruning(true)
    try {
      await api.settings.prune()
      qc.invalidateQueries({ queryKey: qk.stats() })
    } catch {
      /* surfaced by the still-visible banner */
    } finally {
      setPruning(false)
    }
  }

  return (
    <div
      role="alert"
      data-testid="storage-full-banner"
      className="flex items-center gap-3 px-4 py-1.5 bg-danger/15 border-b border-danger/40 text-danger font-mono text-[12px] shrink-0"
    >
      <span className="font-semibold whitespace-nowrap">⚠ Storage full</span>
      <span className="text-foreground/80 min-w-0 truncate">
        Ingestion is paused — Spaniel is returning 503 to exporters. Free disk space, raise max_db_size, or prune old sessions.
      </span>
      <div className="flex-1" />
      <button
        type="button"
        onClick={prune}
        disabled={pruning}
        className="px-2.5 h-[24px] rounded-md border border-danger/50 bg-background text-foreground hover:bg-muted disabled:opacity-60 shrink-0 cursor-pointer"
      >
        {pruning ? 'pruning…' : 'Prune now'}
      </button>
      <NavLink
        to="/settings"
        className="px-2.5 h-[24px] inline-flex items-center rounded-md border border-border bg-background text-foreground hover:bg-muted shrink-0"
      >
        Settings
      </NavLink>
    </div>
  )
}
