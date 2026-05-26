export interface SpanEvent {
  type: 'span'
  traceId: string
  serviceName: string
  name: string
  durationNs: number
  statusCode: number
}

type Handler = (ev: SpanEvent) => void

export function createWS(onEvent: Handler): () => void {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/ws`
  let ws: WebSocket
  let closed = false
  let retryTimeout: ReturnType<typeof setTimeout>

  function connect() {
    ws = new WebSocket(url)
    ws.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as SpanEvent
        onEvent(ev)
      } catch {}
    }
    ws.onclose = () => {
      if (!closed) {
        retryTimeout = setTimeout(connect, 2000)
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
