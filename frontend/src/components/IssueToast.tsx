import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useWS, type IssuePayload } from '@/lib/ws'

export default function IssueToast() {
  const [toasts, setToasts] = useState<Array<{ id: number; payload: IssuePayload }>>([])

  useWS(ev => {
    if (ev.type !== 'issue') return
    const id = Date.now()
    setToasts(prev => [...prev, { id, payload: ev.payload }].slice(-3))
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 5000)
  })

  if (toasts.length === 0) return null

  return (
    <div className="fixed bottom-9 right-4 z-[1000] flex flex-col gap-2 pointer-events-none">
      {toasts.map(({ id, payload }) => (
        <div
          key={id}
          className="bg-danger-bg border border-danger rounded-lg px-3.5 py-2.5 min-w-[260px] max-w-[340px] shadow-[0_2px_12px_rgba(0,0,0,0.18)] pointer-events-auto"
        >
          <div className="flex items-center gap-[7px] mb-1">
            <span className="w-[7px] h-[7px] rounded-full bg-danger shrink-0" />
            <span className="font-mono text-[10px] font-bold uppercase tracking-[0.07em] text-danger-ink">
              {payload.kind.replace(/_/g, ' ')}
            </span>
          </div>
          <div className="font-mono text-[11px] text-ink2 mb-1.5">
            {payload.count} repeated queries detected
          </div>
          <Link
            to={`/traces/${payload.traceId}`}
            className="font-mono text-[10px] text-danger underline decoration-dotted"
          >
            view trace →
          </Link>
        </div>
      ))}
    </div>
  )
}
