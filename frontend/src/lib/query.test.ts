// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createElement, type ReactNode } from 'react'

// Capture the event handler that useLiveInvalidation registers with the socket
// so we can drive it synthetically.
let captured: ((ev: unknown) => void) | null = null
vi.mock('./ws', () => ({
  onWSEvent: (onEvent: (ev: unknown) => void) => {
    captured = onEvent
    return () => {}
  },
}))

import { useLiveInvalidation } from './query'

describe('useLiveInvalidation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    captured = null
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('invalidates the traces query on a span event, throttled by ~1s', () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)

    renderHook(() => useLiveInvalidation(), { wrapper })
    expect(captured).toBeTypeOf('function')

    captured!({ type: 'span', timestamp_ns: 0, payload: {} })
    // coalesced — nothing fires synchronously
    expect(spy).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1_000)
    expect(spy).toHaveBeenCalledWith({ queryKey: ['traces'] })
    expect(spy).toHaveBeenCalledWith({ queryKey: ['dashboard-panel'] })
  })

  it.each(['log', 'metric'])('invalidates dashboard panels for a live %s event', (type) => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({ type, timestamp_ns: 0, payload: {} })
    vi.advanceTimersByTime(1_000)
    expect(spy).toHaveBeenCalledWith({ queryKey: ['dashboard-panel'] })
  })

  it('updates forwarder status from the live event without invalidating its query', () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({
      type: 'forwarder',
      timestamp_ns: 0,
      payload: {
        url: 'http://tempo:4318',
        sent: 12,
        errors: 1,
        lastError: 'connection refused',
        pendingBytes: 64,
        droppedSpool: 2,
      },
    })

    expect(qc.getQueryData(['forwarders'])).toEqual([
      {
        url: 'http://tempo:4318',
        sent: 12,
        errors: 1,
        last_error: 'connection refused',
        pending_bytes: 64,
        dropped_spool: 2,
      },
    ])
    expect(spy).not.toHaveBeenCalled()
  })

  it('ignores throughput events', () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({ type: 'throughput', timestamp_ns: 0, payload: {} })
    vi.advanceTimersByTime(1_000)
    expect(spy).not.toHaveBeenCalled()
  })
})
