// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Notifications from './Notifications'
import { api } from '@/lib/api'
import type { WsEvent } from '@/lib/ws'

let receive: (event: WsEvent) => void
vi.mock('@/lib/ws', () => ({
  useWS: (fn: typeof receive) => {
    receive = fn
  },
  useWSStatus: () => ({ connected: true }),
}))
vi.mock('@/lib/api', () => ({
  api: { notifications: { list: vi.fn(), read: vi.fn(), acknowledge: vi.fn() } },
}))
const notification = {
  id: 'one',
  source: 'alert',
  severity: 'warning',
  title: 'High latency',
  body: 'Slow requests',
  created_at: Date.now() * 1e6,
  link: '/alerts',
  read_at: null,
  acknowledged_at: null,
}
function mount(path = '/notifications') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Notifications />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.notifications.list).mockResolvedValue({
    data: [notification],
    meta: { total: 36, page: 1 },
  } as never)
})
afterEach(cleanup)
it('requests 35 rows, pages via the API and marks a row read', async () => {
  mount()
  await screen.findByText('High latency')
  expect(api.notifications.list).toHaveBeenCalledWith({ page: 1, limit: 35 })
  fireEvent.click(screen.getByRole('button', { name: 'Next notifications page' }))
  await waitFor(() => expect(api.notifications.list).toHaveBeenCalledWith({ page: 2, limit: 35 }))
  fireEvent.click(await screen.findByRole('button', { name: /^Read$/ }))
  await waitFor(() => expect(api.notifications.read).toHaveBeenCalledWith('one'))
})
it('refreshes persisted data when a notification arrives over websocket', async () => {
  mount()
  await screen.findByText('High latency')
  vi.mocked(api.notifications.list).mockResolvedValue({
    data: [{ ...notification, title: 'New live alert' }],
    meta: { total: 1, page: 1 },
  } as never)
  receive({
    type: 'notification',
    timestamp_ns: 1,
    payload: { id: 'two', source: 'alert', sourceId: 'rule' },
  })
  await screen.findByText('New live alert')
})
it('separates the all-notification cache from the alert-only list', async () => {
  const client = mount('/notifications?page=2')
  await screen.findByText('High latency')
  expect(client.getQueryData(['notifications', { page: 2, limit: 35 }])).toBeDefined()
  expect(client.getQueryData(['notifications', { page: 2, source: 'alert' }])).toBeUndefined()
})
it('shows loading errors without claiming the inbox is empty', async () => {
  vi.mocked(api.notifications.list).mockRejectedValue(new Error('offline'))
  mount('/notifications?page=invalid')
  await screen.findByRole('alert')
  expect(screen.queryByText('No notifications yet.')).toBeNull()
  expect(api.notifications.list).toHaveBeenCalledWith({ page: 1, limit: 35 })
})
it('shows an empty inbox', async () => {
  vi.mocked(api.notifications.list).mockResolvedValue({
    data: [],
    meta: { total: 0, page: 1 },
  } as never)
  mount()
  await screen.findByText('No notifications yet.')
  expect(screen.getByRole('button', { name: 'Next notifications page' })).toHaveProperty(
    'disabled',
    true,
  )
})
