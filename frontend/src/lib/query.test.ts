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

import { patchSelfTrace, useLiveInvalidation } from './query'

describe('useLiveInvalidation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    captured = null
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not refetch trace or span lists after a span WebSocket event', () => {
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
    expect(spy).not.toHaveBeenCalledWith({ queryKey: ['traces'] }, { cancelRefetch: false })
    expect(spy).not.toHaveBeenCalledWith({ queryKey: ['spans'] }, { cancelRefetch: false })
    expect(spy).toHaveBeenCalledWith({ queryKey: ['dashboard-panel'] }, { cancelRefetch: false })
  })

  it.each(['log', 'metric'])('invalidates dashboard panels for a live %s event', (type) => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({ type, timestamp_ns: 0, payload: {} })
    vi.advanceTimersByTime(1_000)
    expect(spy).toHaveBeenCalledWith({ queryKey: ['dashboard-panel'] }, { cancelRefetch: false })
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

  it('patches a self trace into the list cache without invalidating it', () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)
    const existing = {
      trace_id: 'existing',
      service_name: 'spaniel',
      name: 'existing',
      attributes: '{}',
      status_code: 0,
      start_ns: 1,
      end_ns: 2,
      duration_ns: 1,
      session_id: 'session',
      session_label: 'session',
      has_n1: false,
      span_count: 1,
    }
    const incoming = { ...existing, trace_id: 'incoming', name: 'storage.AppendSpan', start_ns: 3 }
    qc.setQueryData(['traces', { sessionId: null, service: 'all', page: 1 }], {
      data: [existing],
      meta: { page: 1, total: 1 },
    })

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({ type: 'self_trace', timestamp_ns: 0, payload: incoming })

    expect(
      qc.getQueryData<{ data: (typeof existing)[]; meta: { total: number } }>([
        'traces',
        { sessionId: null, service: 'all', page: 1 },
      ]),
    ).toEqual({ data: [incoming, existing], meta: { page: 1, total: 2 } })
    expect(spy).not.toHaveBeenCalled()
  })

  it('keeps an empty trace page capable of receiving its first self trace', () => {
    const trace = {
      trace_id: 'first',
      service_name: 'spaniel',
      name: 'first',
      attributes: '{}',
      status_code: 0,
      start_ns: 1,
      end_ns: 2,
      duration_ns: 1,
      session_id: 'session',
      session_label: 'session',
      has_n1: false,
      span_count: 1,
    }
    expect(patchSelfTrace({ data: [], meta: { page: 1, total: 0 } }, trace)).toEqual({
      data: [trace],
      meta: { page: 1, total: 1 },
    })
  })

  it('patches self spans and logs without invalidating either list', () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)
    const span = {
      trace_id: 'trace',
      span_id: 'span',
      parent_span_id: '',
      service_name: 'spaniel',
      name: 'storage.AppendSpan',
      kind: 1,
      start_ns: 1,
      end_ns: 2,
      duration_ns: 1,
      status_code: 0,
      status_message: '',
      attributes: '{}',
      resource: '{}',
      session_id: 'session',
      session_label: 'session',
      received_at: 2,
      events: [],
      links: [],
    }
    const log = {
      timestamp_ns: 3,
      trace_id: 'trace',
      span_id: 'span',
      severity: 9,
      body: 'stored',
      attributes: '{}',
      service_name: 'spaniel',
      session_id: 'session',
      received_at: 3,
    }
    qc.setQueryData(['spans', { sort: 'time', page: 1, drilldown: null }], {
      data: [],
      meta: { page: 1, total: 0 },
    })
    qc.setQueryData(
      ['logs', { severity: undefined, traceId: undefined, service: 'all', page: 1 }],
      {
        data: [],
        meta: { page: 1, total: 0 },
      },
    )

    renderHook(() => useLiveInvalidation(), { wrapper })
    captured!({ type: 'self_span', timestamp_ns: 0, payload: span })
    captured!({ type: 'self_log', timestamp_ns: 0, payload: log })

    expect(
      qc.getQueryData<{ data: (typeof span)[] }>([
        'spans',
        { sort: 'time', page: 1, drilldown: null },
      ]),
    ).toMatchObject({ data: [span] })
    expect(
      qc.getQueryData<{ data: (typeof log)[] }>([
        'logs',
        { severity: undefined, traceId: undefined, service: 'all', page: 1 },
      ]),
    ).toMatchObject({ data: [log] })
    expect(spy).not.toHaveBeenCalled()
  })

  it('patches normal span, trace, log, and metric caches from persisted WebSocket rows', () => {
    const qc = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children)
    const span = {
      trace_id: 'trace',
      span_id: 'span',
      parent_span_id: '',
      service_name: 'api',
      name: 'GET /',
      kind: 2,
      start_ns: 1,
      end_ns: 2,
      duration_ns: 1,
      status_code: 0,
      status_message: '',
      attributes: '{}',
      resource: '{}',
      session_id: 'session',
      session_label: 'session',
      received_at: 2,
      events: [],
      links: [],
    }
    const trace = {
      trace_id: 'trace',
      service_name: 'api',
      name: 'GET /',
      attributes: '{}',
      status_code: 0,
      start_ns: 1,
      end_ns: 2,
      duration_ns: 1,
      session_id: 'session',
      session_label: 'session',
      has_n1: false,
      span_count: 1,
      issue_kinds: [],
    }
    const log = {
      timestamp_ns: 3,
      trace_id: 'trace',
      span_id: 'span',
      severity: 9,
      body: 'ready',
      attributes: '{}',
      service_name: 'api',
      session_id: 'session',
      received_at: 3,
    }
    qc.setQueryData(['spans', { sort: 'time', page: 1, services: [], sessionId: null }], {
      data: [],
      meta: { page: 1, total: 0 },
    })
    qc.setQueryData(['traces', { page: 1, services: [], sessionId: null }], {
      data: [],
      meta: { page: 1, total: 0 },
    })
    qc.setQueryData(['logs', { page: 1, services: [], sessionId: null }], {
      data: [],
      meta: { page: 1, total: 0 },
    })
    qc.setQueryData(['metrics', null], [])
    renderHook(() => useLiveInvalidation(), { wrapper })

    captured!({ type: 'span', timestamp_ns: 2, payload: { row: span, trace } })
    captured!({ type: 'log', timestamp_ns: 3, payload: { row: log } })
    captured!({
      type: 'metric',
      timestamp_ns: 4,
      payload: {
        row: {
          name: 'requests',
          description: '',
          unit: '1',
          type: 'gauge',
          timestamp_ns: 4,
          value: 7,
          attributes: '{}',
          series_attributes: '{}',
          service_name: 'api',
          session_id: 'session',
        },
      },
    })

    expect(
      qc.getQueryData<{ data: (typeof span)[] }>([
        'spans',
        { sort: 'time', page: 1, services: [], sessionId: null },
      ])?.data,
    ).toEqual([span])
    expect(
      qc.getQueryData<{ data: (typeof trace)[] }>([
        'traces',
        { page: 1, services: [], sessionId: null },
      ])?.data,
    ).toEqual([trace])
    expect(
      qc.getQueryData<{ data: (typeof log)[] }>([
        'logs',
        { page: 1, services: [], sessionId: null },
      ])?.data,
    ).toEqual([log])
    expect(qc.getQueryData<{ name: string }[]>(['metrics', null])?.[0]?.name).toBe('requests')
  })
})
