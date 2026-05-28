import { useEffect, useState } from 'react'

export interface SpanPayload { traceId: string; spanId: string; serviceName: string; name: string; durationNs: number; statusCode: number }
export interface LogPayload { traceId: string; spanId: string; severity: number; body: string; serviceName: string; sessionId: string }
export interface MetricPayload { name: string; serviceName: string; value: number; type: string }
export interface IssuePayload { traceId: string; kind: string; fingerprint: string; count: number; wastedNs: number }
export interface ForwarderPayload { url: string; sent: number; errors: number; lastError?: string }
export interface ThroughputPayload { spansPerSec: number; logsPerSec: number }

export type WsEvent =
  | { type: 'span';       timestamp_ns: number; payload: SpanPayload }
  | { type: 'log';        timestamp_ns: number; payload: LogPayload }
  | { type: 'metric';     timestamp_ns: number; payload: MetricPayload }
  | { type: 'issue';      timestamp_ns: number; payload: IssuePayload }
  | { type: 'forwarder';  timestamp_ns: number; payload: ForwarderPayload }
  | { type: 'throughput'; timestamp_ns: number; payload: ThroughputPayload }

// Keep SpanEvent as a backward-compat alias:
export type SpanEvent = Extract<WsEvent, { type: 'span' }>

type Handler = (ev: WsEvent) => void
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
        const ev = JSON.parse(e.data)
        if (typeof ev?.type !== 'string') return
        onEvent(ev as WsEvent)
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
