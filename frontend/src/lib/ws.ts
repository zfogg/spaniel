import { useEffect, useState } from 'react'

export interface SpanEvent {
  type: 'span'
  traceId: string
  spanId: string
  serviceName: string
  name: string
  durationNs: number
  statusCode: number
}

type Handler = (ev: SpanEvent) => void
type StatusHandler = (connected: boolean) => void

const MIN_BACKOFF_MS = 500
const MAX_BACKOFF_MS = 30_000

export function createWS(onEvent: Handler, onStatus?: StatusHandler): () => void {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/ws`
  let ws: WebSocket
  let closed = false
  let retryTimeout: ReturnType<typeof setTimeout>
  let backoff = MIN_BACKOFF_MS

  function connect() {
    ws = new WebSocket(url)

    ws.onopen = () => {
      backoff = MIN_BACKOFF_MS
      onStatus?.(true)
    }

    ws.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as SpanEvent
        onEvent(ev)
      } catch {}
    }

    ws.onclose = () => {
      onStatus?.(false)
      if (!closed) {
        retryTimeout = setTimeout(() => {
          backoff = Math.min(backoff * 2, MAX_BACKOFF_MS)
          connect()
        }, backoff)
      }
    }

    ws.onerror = () => ws.close()
  }

  connect()

  return () => {
    closed = true
    clearTimeout(retryTimeout)
    ws?.close()
  }
}

export function useWS(onEvent: Handler, onStatus?: StatusHandler) {
  useEffect(() => createWS(onEvent, onStatus), [])
}

// Hook for components that only care about connection status.
export function useWSStatus(): boolean {
  const [connected, setConnected] = useState(false)
  useEffect(() => createWS(() => {}, setConnected), [])
  return connected
}
