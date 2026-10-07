// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { DashboardEditor } from './Dashboards'
import { api } from '@/lib/api'
import { dashboardTemplates } from '@/components/dashboard-panels/dashboard-templates'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  sessionStorage.clear()
})
function setup() {
  vi.spyOn(api.dashboards, 'list').mockResolvedValue({ data: [], meta: { total: 0 } })
  vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
  const create = vi.spyOn(api.dashboards, 'create').mockResolvedValue({
    data: {
      id: 'created',
      name: 'Office',
      description: '',
      panels: [],
      variables: [],
      created_at: 0,
      updated_at: 0,
    },
    meta: { total: 1 },
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/dashboards/new']}>
        <Routes>
          <Route path="/dashboards/new" element={<DashboardEditor />} />
          <Route path="/dashboards/:dashboardId" element={<p>Saved dashboard</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return create
}
it('creates None with no panels', async () => {
  const create = setup()
  fireEvent.click(screen.getByRole('button', { name: 'Create dashboard' }))
  await screen.findByText('Saved dashboard')
  expect(create).toHaveBeenCalledWith(expect.objectContaining({ panels: [] }))
})
it('sends only the chosen template after switching, preserving custom name', async () => {
  const create = setup()
  fireEvent.change(screen.getByRole('textbox', { name: /^Name$/ }), {
    target: { value: 'My office' },
  })
  fireEvent.click(screen.getByRole('radio', { name: 'Service overview' }))
  fireEvent.click(screen.getByRole('radio', { name: 'Queue worker' }))
  fireEvent.click(screen.getByRole('button', { name: 'Create dashboard' }))
  await screen.findByText('Saved dashboard')
  expect(create).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      name: 'My office',
      panels: dashboardTemplates.find((t) => t.id === 'queue')!.panels,
    }),
  )
})
it('preserves selection and shows an error without pretending creation succeeded', async () => {
  const create = setup().mockRejectedValue(new Error('Database unavailable'))
  fireEvent.click(screen.getByRole('radio', { name: 'Database health' }))
  fireEvent.click(screen.getByRole('button', { name: 'Create dashboard' }))
  await waitFor(() =>
    expect(screen.getByRole('alert').textContent).toContain('Database unavailable'),
  )
  expect((screen.getByRole('radio', { name: 'Database health' }) as HTMLInputElement).checked).toBe(
    true,
  )
  expect(create).toHaveBeenCalledTimes(1)
  expect(screen.queryByText('Saved dashboard')).toBeNull()
})
