import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'

export function UpdatedBadge({ updated }: { updated?: boolean }) {
  return updated ? (
    <span className="rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-blue-700 dark:text-blue-300">
      Updated
    </span>
  ) : null
}

// Keep the badge visible during the first visit, then refresh the list when
// leaving. Detail reads acknowledge the exact server revision across clients.
export function DefinitionUpdatedBadge({
  kind,
  id,
  revision,
  updated,
}: {
  kind: 'dashboard' | 'alert'
  id: string
  revision: string
  updated?: boolean
}) {
  const qc = useQueryClient()
  const visit = useRef({ revision, updated })
  if (visit.current.revision !== revision) visit.current = { revision, updated }
  useEffect(() => {
    if (updated) {
      const request = kind === 'dashboard' ? api.dashboards.get(id) : api.alerts.get(id)
      // Keep unread on failure; a later visit can retry.
      void request.catch(() => {})
    }
    return () => {
      void qc.invalidateQueries({ queryKey: [kind === 'dashboard' ? 'dashboards' : 'alerts'] })
    }
  }, [kind, id, revision, qc, updated])
  return <UpdatedBadge updated={visit.current.updated} />
}
