// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api, type DashboardPanel } from '@/lib/api'
import { qk } from '@/lib/query'
import { PanelRenderer } from './PanelRenderer'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('refetches a current-session panel when the active session changes', async () => {
  vi.spyOn(api.sessions, 'getActive').mockResolvedValue({
    data: { id: 'first', label: 'First' },
    meta: { total: 1 },
  })
  const preview = vi.spyOn(api.dashboards, 'preview').mockResolvedValue({
    data: { columns: ['value'], rows: [{ value: 1 }], warnings: [] },
    meta: { total: 1 },
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(qk.activeSession(), { id: 'first', label: 'First' })
  const panel = {
    id: 'dynamic',
    query_sql: 'SELECT count(*) AS value FROM spans WHERE session_id = $session_id',
    display_type: 'single_value',
    title: 'Current spans',
  } as DashboardPanel
  render(
    <QueryClientProvider client={client}>
      <PanelRenderer dashboardId="dashboard" panel={panel} />
    </QueryClientProvider>,
  )
  await waitFor(() => expect(preview).toHaveBeenCalledTimes(1))
  act(() => client.setQueryData(qk.activeSession(), { id: 'second', label: 'Second' }))
  await waitFor(() => expect(preview).toHaveBeenCalledTimes(2))
  expect(preview).toHaveBeenLastCalledWith(
    'dashboard',
    expect.objectContaining({ query_sql: panel.query_sql, variables: {} }),
  )
})
