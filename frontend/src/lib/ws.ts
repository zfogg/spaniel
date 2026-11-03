import { authenticatedFetch, getAuthState, requireAuthentication } from './auth'
import { useEffect, useRef, useState } from 'react'

export type * from '../generated/liveevent'
import { decodeEvent } from '../generated/liveevent'
import type { WsEvent } from '../generated/liveevent'

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
  const sendVisibility = () => {
    if (ws?.readyState === WebSocket.OPEN)
      ws.send(
        JSON.stringify({ type: 'visibility', visible: document.visibilityState !== 'hidden' }),
      )
  }
  document.addEventListener('visibilitychange', sendVisibility)

  // Force a reconnect if the link goes silent (no heartbeat or data). onclose
  // won't always fire promptly on a dead connection, so we watch for silence.
  const staleTimer = setInterval(() => {
    if (closed || !ws || ws.readyState !== WebSocket.OPEN) return
    if (Date.now() - lastActivity > STALE_MS) ws.close()
  }, STALE_CHECK_MS)

  async function connect() {
    if (getAuthState().required) await requireAuthentication(getAuthState().code)
    if (closed) return
    ws = new WebSocket(url)

    ws.onopen = () => {
      sendVisibility()
      backoff = MIN_BACKOFF_MS
      lastActivity = Date.now()
      onStatus?.(true)
    }

    ws.onmessage = (e) => {
      lastActivity = Date.now()
      try {
        const ev = decodeEvent(JSON.parse(e.data))
        if (!ev) return
        onEvent(ev)
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
          void authenticatedFetch('/api/security/session')
            .catch(() => undefined)
            .finally(() => {
              if (!closed) void connect()
            })
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
    document.removeEventListener('visibilitychange', sendVisibility)
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

/** Observe the same shared socket; immediately reports its current state. */
export function onWSStatus(fn: SharedStatusListener): () => void {
  ensureSharedWS()
  statusListeners.add(fn)
  fn(sharedStatus)
  return () => {
    statusListeners.delete(fn)
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
