// @vitest-environment jsdom
import { act, render, cleanup } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { WsEvent } from '@/lib/ws'

const mocks = vi.hoisted(() => ({
  notify: vi.fn(),
  receive: null as null | ((ev: WsEvent) => void),
}))
vi.mock('@/lib/ws', () => ({
  useWS: (fn: (ev: WsEvent) => void) => {
    mocks.receive = fn
  },
}))
vi.mock('./NotificationToast', () => ({ showNotification: mocks.notify }))
import IssueToast from './IssueToast'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  mocks.notify.mockClear()
})

function issue(index: number) {
  mocks.receive?.({
    type: 'issue',
    timestamp_ns: index,
    payload: {
      traceId: `trace-${index}`,
      kind: 'error_chain',
      count: 1,
      fingerprint: 'error',
      wastedNs: 0,
    },
  })
}

it('coalesces a burst across traces and replaces the same toast in later windows', () => {
  vi.useFakeTimers()
  render(<IssueToast />)
  act(() => {
    mocks.receive?.({ type: 'heartbeat', timestamp_ns: 0 })
    vi.advanceTimersByTime(10_000)
  })
  expect(mocks.notify).not.toHaveBeenCalled()
  act(() => {
    for (let i = 0; i < 500; i++) issue(i)
  })
  expect(mocks.notify).not.toHaveBeenCalled()
  act(() => {
    vi.advanceTimersByTime(10_000)
  })
  expect(mocks.notify).toHaveBeenCalledTimes(1)
  expect(mocks.notify).toHaveBeenLastCalledWith(
    expect.objectContaining({
      dedupeKey: 'live-performance-issues',
      link: '/traces/trace-499',
      detail: '500 issue updates · latest: error chain (1)',
    }),
  )
  act(() => {
    issue(500)
    vi.advanceTimersByTime(10_000)
  })
  expect(mocks.notify).toHaveBeenCalledTimes(2)
  expect(mocks.notify).toHaveBeenLastCalledWith(
    expect.objectContaining({
      dedupeKey: 'live-performance-issues',
      detail: '1 issue update · latest: error chain (1)',
    }),
  )
})

it('cancels pending notifications when unmounted', () => {
  vi.useFakeTimers()
  const view = render(<IssueToast />)
  act(() => {
    issue(1)
  })
  view.unmount()
  act(() => {
    vi.advanceTimersByTime(10_000)
  })
  expect(mocks.notify).not.toHaveBeenCalled()
})
