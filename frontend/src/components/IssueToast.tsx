import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import { useWS } from '@/lib/ws'

// Listener-only: turns live `issue` events into sonner toasts. The <Toaster />
// that renders them lives in App.tsx. Uses toast.custom so we keep the Drift
// card styling and the "view trace" link.
export default function IssueToast() {
  useWS(ev => {
    if (ev.type !== 'issue') return
    const p = ev.payload
    toast.custom(id => (
      <div
        data-testid="issue-toast"
        className="bg-danger-bg border border-danger rounded-lg px-3.5 py-2.5 min-w-[260px] max-w-[340px] shadow-[0_2px_12px_rgba(0,0,0,0.18)]"
      >
        <div className="flex items-center gap-[7px] mb-1">
          <span className="w-[7px] h-[7px] rounded-full bg-danger shrink-0" />
          <span className="font-mono text-[10px] font-bold uppercase tracking-[0.07em] text-danger-ink">
            {p.kind.replace(/_/g, ' ')}
          </span>
        </div>
        <div className="font-mono text-[11px] text-ink2 mb-1.5">
          {p.count} repeated queries detected
        </div>
        <Link
          to={`/traces/${p.traceId}`}
          onClick={() => toast.dismiss(id)}
          className="font-mono text-[10px] text-danger underline decoration-dotted"
        >
          view trace →
        </Link>
      </div>
    ), { duration: 5000 })
  })

  return null
}
