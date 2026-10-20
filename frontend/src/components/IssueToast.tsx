import { useEffect, useRef } from 'react'
import { useWS } from '@/lib/ws'
import type { IssuePayload } from '@/lib/ws'
import { showNotification } from './NotificationToast'

// Listener-only: turns live `issue` events into sonner toasts. The <Toaster />
// that renders them lives in App.tsx. Uses toast.custom so we keep the Drift
// card styling and the "view trace" link.
export default function IssueToast() {
  const pending = useRef<{ updates: number; latest: IssuePayload } | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current)
    },
    [],
  )

  useWS((ev) => {
    if (ev.type !== 'issue') return
    pending.current = { updates: (pending.current?.updates ?? 0) + 1, latest: ev.payload }
    if (timer.current !== null) return
    // Live traces are reanalysed as spans arrive. Coalesce those updates and
    // replace one toast; individual issues remain available on each trace.
    timer.current = setTimeout(() => {
      const batch = pending.current
      pending.current = null
      timer.current = null
      if (!batch) return
      const p = batch.latest
      const kind = p.kind.replace(/_/g, ' ')
      showNotification({
        title: 'Performance issues',
        detail: `${batch.updates} issue ${batch.updates === 1 ? 'update' : 'updates'} · latest: ${kind} (${p.count})`,
        link: `/traces/${p.traceId}`,
        linkLabel: 'view latest trace →',
        severity: 'warning',
        dedupeKey: 'live-performance-issues',
      })
    }, 10_000)
  })

  return null
}
