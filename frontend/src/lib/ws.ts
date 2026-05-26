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

const MIN_BACKOFF_MS = 500
const MAX_BACKOFF_MS = 30_000

export function createWS(onEvent: Handler): () => void {
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
    }

    ws.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as SpanEvent
        onEvent(ev)
      } catch {}
    }

    ws.onclose = () => {
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
