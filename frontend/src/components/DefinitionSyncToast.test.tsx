// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { WsEvent } from '@/lib/ws'
import { toast } from 'sonner'
import AlertToast from './AlertToast'

let receive: (event: WsEvent) => void
vi.mock('@/lib/ws', () => ({
  useWS: (handler: typeof receive) => {
    receive = handler
  },
}))
vi.mock('sonner', () => ({ toast: { loading: vi.fn(), success: vi.fn(), error: vi.fn() } }))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})
it('shows progress, success, and errors and refreshes definition queries', () => {
  const qc = new QueryClient()
  const invalidate = vi.spyOn(qc, 'invalidateQueries')
  render(
    <QueryClientProvider client={qc}>
      <AlertToast />
    </QueryClientProvider>,
  )
  const send = (kind: 'dashboard' | 'alert', status: 'updating' | 'updated' | 'error') =>
    receive({
      type: 'definition_sync',
      timestamp_ns: 1,
      payload: { kind, status, message: 'Invalid YAML' },
    })
  send('dashboard', 'updating')
  expect(toast.loading).toHaveBeenCalled()
  send('dashboard', 'updated')
  expect(toast.success).toHaveBeenCalled()
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboards'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboard-panel'] })
  send('alert', 'updated')
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['alerts'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['alert-rule'] })
  invalidate.mockClear()
  send('alert', 'error')
  expect(toast.error).toHaveBeenCalled()
  expect(invalidate).not.toHaveBeenCalled()
})
