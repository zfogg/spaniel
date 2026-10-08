import { useEffect, useRef, useState } from 'react'

export interface SpanPayload {
  traceId: string
  spanId: string
  serviceName: string
  name: string
  durationNs: number
  statusCode: number
  sessionId: string
}
export type SelfTracePayload = import('./api').TraceRow
export interface LogPayload {
  traceId: string
  spanId: string
  severity: number
  body: string
  serviceName: string
  sessionId: string
}
export interface MetricPayload {
  name: string
  serviceName: string
  value: number
  type: string
  catalogOnly?: boolean
}
export interface IssuePayload {
  traceId: string
  kind: string
  fingerprint: string
  count: number
  wastedNs: number
}
export interface ForwarderPayload {
  url: string
  sent: number
  errors: number
  lastError?: string
  pendingBytes?: number
  droppedSpool?: number
}
export interface ThroughputPayload {
  spansPerSec: number
  logsPerSec: number
}
export interface LiveStatePayload {
  stats: import('./api').Stats
  sources: import('./api').SourceStats[]
}
export interface ActiveSessionPayload {
  id: string
  label: string
}
export interface AlertPayload {
  ruleId: string
  ruleName: string
  severity: 'info' | 'warning' | 'critical' | string
  state: 'pending' | 'firing' | 'resolved' | 'error' | string
  transition: 'firing' | 'resolved' | 'acknowledged' | string
  groupLabels: Record<string, string>
  currentValue?: number | null
  threshold?: number
  operator?: string
  link?: string
  browser?: boolean
}
export interface AlertSyncPayload {
  ruleId: string
  reason: string
  events?: import('./api').AlertEvent[]
  total?: number
}
export interface NotificationPayload {
  id: string
  source: string
  sourceId: string
}

export type WsEvent =
  | { type: 'span'; timestamp_ns: number; payload: SpanPayload }
  | { type: 'self_trace'; timestamp_ns: number; payload: SelfTracePayload }
  | { type: 'log'; timestamp_ns: number; payload: LogPayload }
  | { type: 'metric'; timestamp_ns: number; payload: MetricPayload }
  | { type: 'issue'; timestamp_ns: number; payload: IssuePayload }
  | { type: 'forwarder'; timestamp_ns: number; payload: ForwarderPayload }
  | { type: 'throughput'; timestamp_ns: number; payload: ThroughputPayload }
  | { type: 'live_state'; timestamp_ns: number; payload: LiveStatePayload }
  | { type: 'active_session'; timestamp_ns: number; payload: ActiveSessionPayload }
  | { type: 'alert'; timestamp_ns: number; payload: AlertPayload }
  | { type: 'alert_sync'; timestamp_ns: number; payload: AlertSyncPayload }
  | { type: 'notification'; timestamp_ns: number; payload: NotificationPayload }
  | { type: 'heartbeat'; timestamp_ns: number }

// Keep SpanEvent as a backward-compat alias:
export type SpanEvent = Extract<WsEvent, { type: 'span' }>

type Handler = (ev: WsEvent) => void
type StatusHandler = (connected: boolean) => void

const MIN_BACKOFF_MS = 500
const MAX_BACKOFF_MS = 30_000
// If no frame (heartbeat or data) arrives for this long, treat the socket as
// dead and force a reconnect. The server heartbeats every 5s, so 15s tolerates
// a couple of missed beats before we flag a disconnect.
const STALE_MS = 15_000
const STALE_CHECK_MS = 3_000

export function createWS(onEvent: Handler, onStatus?: StatusHandler): () => void {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/ws`
  let ws: WebSocket
  let closed = false
  let retryTimeout: ReturnType<typeof setTimeout>
  let backoff = MIN_BACKOFF_MS
  let lastActivity = Date.now()

  // Force a reconnect if the link goes silent (no heartbeat or data). onclose
  // won't always fire promptly on a dead connection, so we watch for silence.
  const staleTimer = setInterval(() => {
    if (closed || !ws || ws.readyState !== WebSocket.OPEN) return
    if (Date.now() - lastActivity > STALE_MS) ws.close()
  }, STALE_CHECK_MS)

  function connect() {
    ws = new WebSocket(url)

    ws.onopen = () => {
      backoff = MIN_BACKOFF_MS
      lastActivity = Date.now()
      onStatus?.(true)
    }

    ws.onmessage = (e) => {
      lastActivity = Date.now()
      try {
        const ev = JSON.parse(e.data)
        if (typeof ev?.type !== 'string') return
        onEvent(ev as WsEvent)
      } catch (err) {
        // A malformed frame is a backend bug, not something to silently eat —
        // log it (with a snippet of the payload) so it's debuggable, but keep
        // the socket alive so one bad frame doesn't kill the live stream.
        console.error('[ws] dropping malformed message', err, String(e.data).slice(0, 200))
      }
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
    clearInterval(staleTimer)
    ws?.close()
  }
}

export function useWS(onEvent: Handler, onStatus?: StatusHandler) {
  // Keep the latest callbacks in refs so subscriptions to the one app-wide
  // socket always call current handlers rather than stale render closures.
  const evRef = useRef(onEvent)
  evRef.current = onEvent
  const stRef = useRef(onStatus)
  stRef.current = onStatus
  useEffect(() => {
    const unsub = onWSEvent((e) => evRef.current(e))
    const onSharedStatus = (status: WSStatus) => stRef.current?.(status.connected)
    if (stRef.current) {
      statusListeners.add(onSharedStatus)
      onSharedStatus(sharedStatus)
    }
    return () => {
      unsub()
      statusListeners.delete(onSharedStatus)
      releaseSharedWS()
    }
  }, [])
}

// ── Shared event bus ──────────────────────────────────────────────────────────
// A single WebSocket feeds this bus; hooks subscribe via `onWSEvent`.
// This avoids multiple WS connections when both useLiveInvalidation and
// useLiveActivity are mounted.

type Listener = (ev: WsEvent) => void
type SharedStatusListener = (status: WSStatus) => void
const listeners = new Set<Listener>()
const statusListeners = new Set<SharedStatusListener>()
let sharedDisconnect: (() => void) | null = null
let sharedStopTimer: ReturnType<typeof setTimeout> | null = null
let sharedStatus: WSStatus = { connected: false, since: null }

function ensureSharedWS() {
  if (sharedStopTimer) {
    clearTimeout(sharedStopTimer)
    sharedStopTimer = null
  }
  if (sharedDisconnect) return
  sharedDisconnect = createWS(
    (ev) => {
      for (const fn of listeners) fn(ev)
    },
    (connected) => {
      sharedStatus = connected
        ? { connected: true, since: Date.now() }
        : { connected: false, since: null }
      for (const fn of statusListeners) fn(sharedStatus)
    },
  )
}

function releaseSharedWS() {
  if (listeners.size > 0 || statusListeners.size > 0 || sharedStopTimer) return
  // React Strict Mode immediately tears down and recreates effects in development.
  // Defer closing until the next task so that probe does not create a second /ws
  // upgrade for the same app instance.
  sharedStopTimer = setTimeout(() => {
    sharedStopTimer = null
    if (listeners.size > 0 || statusListeners.size > 0 || !sharedDisconnect) return
    sharedDisconnect()
    sharedDisconnect = null
    sharedStatus = { connected: false, since: null }
  }, 0)
}

/** Subscribe to the shared WebSocket event stream. Returns an unsubscribe fn. */
export function onWSEvent(fn: Listener): () => void {
  ensureSharedWS()
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
    releaseSharedWS()
  }
}

export interface WSStatus {
  connected: boolean
  // Epoch ms when the current connection opened, or null while disconnected.
  since: number | null
}

// Hook for components that care about connection status and how long the
// current connection has been up. `since` resets on every (re)connect.
export function useWSStatus(): WSStatus {
  const [status, setStatus] = useState<WSStatus>(sharedStatus)
  useEffect(() => {
    ensureSharedWS()
    statusListeners.add(setStatus)
    setStatus(sharedStatus)
    return () => {
      statusListeners.delete(setStatus)
      releaseSharedWS()
    }
  }, [])
  return status
}
