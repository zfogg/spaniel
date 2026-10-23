// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import type { WsEvent } from './ws'
const socket = vi.hoisted(() => ({
  handler: undefined as undefined | ((event: WsEvent) => void),
  connected: true,
}))
vi.mock('./ws', () => ({
  onWSEvent: (handler: (event: WsEvent) => void) => {
    socket.handler = handler
    return () => {
      socket.handler = undefined
    }
  },
  useWSStatus: () => ({ connected: socket.connected }),
}))
import { useLiveTrace } from './use-live-trace'
beforeEach(() => {
  vi.useFakeTimers()
  socket.connected = true
})
afterEach(() => vi.useRealTimers())
function setup() {
  const client = new QueryClient()
  const keys = [
    ['trace', 'target'],
    ['trace', 'other'],
    ['issues', 'target'],
    ['lint', null],
    ['logs', { traceId: 'target', page: 2 }],
    ['logs', { traceId: 'other' }],
    ['trace-span', { traceId: 'target', spanId: 'selected' }],
    ['trace-incoming-links', 'target'],
  ]
  for (const key of keys) client.setQueryData(key, [])
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const hook = renderHook(({ traceId }) => useLiveTrace(traceId), {
    wrapper,
    initialProps: { traceId: 'target' },
  })
  return { client, hook }
}
it.each(['span', 'log', 'issue', 'self_span', 'self_trace', 'self_log'])(
  'refreshes all trace resources for %s events, coalescing bursts',
  (type) => {
    const { client, hook } = setup()
    const spy = vi.spyOn(client, 'invalidateQueries')
    act(() => {
      for (let i = 0; i < 5; i++)
        socket.handler!({
          type,
          payload: { traceId: 'target', trace_id: 'target' },
        } as unknown as WsEvent)
      vi.advanceTimersByTime(1000)
    })
    expect(spy).toHaveBeenCalledTimes(1)
    for (const [key, state] of client
      .getQueryCache()
      .getAll()
      .map((query) => [query.queryKey, query.state] as const))
      expect(state.isInvalidated).toBe(
        key[1] !== 'other' &&
          !(
            typeof key[1] === 'object' &&
            key[1] &&
            'traceId' in key[1] &&
            key[1].traceId === 'other'
          ),
      )
    hook.unmount()
  },
)
it('ignores unrelated logs, cancels pending work on route changes and unmount', () => {
  const { client, hook } = setup()
  const spy = vi.spyOn(client, 'invalidateQueries')
  act(() => {
    socket.handler!({ type: 'log', payload: { traceId: 'other' } } as WsEvent)
    vi.advanceTimersByTime(1000)
  })
  expect(spy).not.toHaveBeenCalled()
  act(() => {
    socket.handler!({ type: 'log', payload: { traceId: 'target' } } as WsEvent)
  })
  hook.rerender({ traceId: 'next' })
  act(() => vi.advanceTimersByTime(1000))
  expect(spy).not.toHaveBeenCalled()
  hook.unmount()
  expect(socket.handler).toBeUndefined()
})
it('refreshes once when a connection recovers', () => {
  const { client, hook } = setup()
  const spy = vi.spyOn(client, 'invalidateQueries')
  socket.connected = false
  hook.rerender({ traceId: 'target' })
  socket.connected = true
  hook.rerender({ traceId: 'target' })
  expect(spy).toHaveBeenCalledTimes(1)
  hook.unmount()
})
