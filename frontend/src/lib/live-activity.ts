import { useEffect, useRef, useState } from 'react'
import { onWSEvent, SpanPayload } from './ws'

/**
 * Tracks live WebSocket span activity via the shared event bus.
 *
 * - `streaming`: true when span events have arrived in the last 3 seconds
 * - `activeSessionId`: the session ID of the most recent span event
 */
export function useLiveActivity() {
  const [streaming, setStreaming] = useState(false)
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return onWSEvent((ev) => {
      if (ev.type !== 'span') return
      const payload = ev.payload as SpanPayload & { sessionId?: string }
      if (!payload.sessionId) return

      setActiveSessionId(payload.sessionId)

      setStreaming(true)
      if (timerRef.current) clearTimeout(timerRef.current)
      timerRef.current = setTimeout(() => setStreaming(false), 3_000)
    })
  }, [])

  useEffect(() => () => { if (timerRef.current) clearTimeout(timerRef.current) }, [])

  return { streaming, activeSessionId }
}
