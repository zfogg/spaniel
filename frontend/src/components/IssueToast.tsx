import { useWS } from '@/lib/ws'
import { showNotification } from './NotificationToast'

// Listener-only: turns live `issue` events into sonner toasts. The <Toaster />
// that renders them lives in App.tsx. Uses toast.custom so we keep the Drift
// card styling and the "view trace" link.
export default function IssueToast() {
  useWS((ev) => {
    if (ev.type !== 'issue') return
    const p = ev.payload
    showNotification({
      title: p.kind.replace(/_/g, ' '),
      detail: `${p.count} repeated queries detected`,
      link: `/traces/${p.traceId}`,
      linkLabel: 'view trace →',
      severity: 'critical',
      dedupeKey: `issue:${p.kind}:${p.traceId}`,
    })
  })

  return null
}
