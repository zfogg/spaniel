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
    <div
      style={{
        position: 'fixed',
        bottom: 36,
        right: 16,
        zIndex: 1000,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        pointerEvents: 'none',
      }}
    >
      {toasts.map(({ id, payload }) => (
        <div
          key={id}
          style={{
            background: 'var(--danger-bg)',
            border: '1px solid var(--danger)',
            borderRadius: 8,
            padding: '10px 14px',
            minWidth: 260,
            maxWidth: 340,
            boxShadow: '0 2px 12px rgba(0,0,0,0.18)',
            pointerEvents: 'auto',
          }}
        >
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: 7,
            marginBottom: 4,
          }}>
            <span style={{
              width: 7,
              height: 7,
              borderRadius: '50%',
              background: 'var(--danger)',
              flexShrink: 0,
            }} />
            <span style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              fontWeight: 700,
              textTransform: 'uppercase',
              letterSpacing: '0.07em',
              color: 'var(--danger-ink)',
            }}>
              {payload.kind.replace(/_/g, ' ')}
            </span>
          </div>
          <div style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--ink2)',
            marginBottom: 6,
          }}>
            {payload.count} repeated queries detected
          </div>
          <Link
            to={`/traces/${payload.traceId}`}
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--danger)',
              textDecoration: 'underline',
              textDecorationStyle: 'dotted',
            }}
          >
            view trace →
          </Link>
        </div>
      ))}
    </div>
  )
}
