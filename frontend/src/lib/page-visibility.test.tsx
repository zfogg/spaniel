// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createElement, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useVisibleQuery } from './page-visibility'
import { useLiveInvalidation } from './query'

let receive: ((event: unknown) => void) | undefined
vi.mock('./ws', () => ({
  onWSEvent: (listener: (event: unknown) => void) => {
    receive = listener
    return () => {
      receive = undefined
    }
  },
}))

function visibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('view-only query work', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    visibility('visible')
  })
  afterEach(() => {
    vi.useRealTimers()
    visibility('visible')
  })

  function view(queryFn: () => Promise<string>) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children)
    const hook = renderHook(
      () => {
        useLiveInvalidation()
        return useVisibleQuery({
          queryKey: ['dashboard-panel', 'test'],
          queryFn,
          staleTime: Infinity,
          refetchInterval: 1000,
        })
      },
      { wrapper },
    )
    return { client, ...hook }
  }

  it('stops hidden timers/events, catches up once, and stops after leaving the view', async () => {
    const fetch = vi.fn(async () => 'data')
    const { unmount } = view(fetch)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(fetch).toHaveBeenCalledTimes(1)
    act(() => visibility('hidden'))
    act(() => {
      for (let i = 0; i < 100; i++) receive?.({ type: 'span', payload: {}, timestamp_ns: i })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(fetch).toHaveBeenCalledTimes(1)
    act(() => visibility('visible'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(fetch).toHaveBeenCalledTimes(2)
    unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('does not start its first query in a background tab', async () => {
    visibility('hidden')
    const fetch = vi.fn(async () => 'data')
    const { unmount } = view(fetch)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetch).not.toHaveBeenCalled()
    act(() => visibility('visible'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(fetch).toHaveBeenCalledTimes(1)
    unmount()
  })

  it('aborts an expensive in-flight panel request when hidden', async () => {
    const abort = vi.fn()
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children)
    const { unmount } = renderHook(
      () => {
        useLiveInvalidation()
        return useVisibleQuery({
          queryKey: ['dashboard-panel', 'slow'],
          queryFn: ({ signal }) =>
            new Promise<string>((_, reject) =>
              signal.addEventListener('abort', () => {
                abort()
                reject(new DOMException('Canceled', 'AbortError'))
              }),
            ),
        })
      },
      { wrapper },
    )
    act(() => visibility('hidden'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(abort).toHaveBeenCalledTimes(1)
    expect(client.getQueryState(['dashboard-panel', 'slow'])?.fetchStatus).toBe('idle')
    unmount()
  })
})
