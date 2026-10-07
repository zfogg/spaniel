// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Dashboards from './Dashboards'
import { api } from '@/lib/api'
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
function Location() {
  return <output data-testid="url">{useLocation().search}</output>
}
it('links selection to the URL and saves a dragged dashboard order', async () => {
  let dashboards = ['First', 'Second', 'Third'].map((id) => ({
    id,
    name: id,
    panels: [],
    variables: [],
    description: '',
    created_at: 0,
    updated_at: 0,
  }))
  vi.spyOn(api.dashboards, 'list').mockImplementation(async () => ({
    data: dashboards,
    meta: { total: 3 },
  }))
  const reorder = vi.spyOn(api.dashboards, 'reorder').mockImplementation(async (ids) => {
    dashboards = ids.map((id) => dashboards.find((d) => d.id === id)!)
    return { data: { ok: true }, meta: { total: 1 } }
  })
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/dashboards']}>
        <Location />
        <Dashboards />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  fireEvent.click(await screen.findByRole('link', { name: 'Second' }))
  expect(screen.getByTestId('url').textContent).toBe('?id=Second')
  expect(screen.getByRole('heading', { name: 'Second' })).toBeTruthy()
  const dataTransfer = { setData: vi.fn(), effectAllowed: '', dropEffect: '' }
  fireEvent.dragStart(screen.getByRole('button', { name: 'Reorder Third' }), { dataTransfer })
  fireEvent.dragOver(screen.getByRole('link', { name: 'First' }), { dataTransfer })
  fireEvent.drop(screen.getByRole('link', { name: 'First' }), { dataTransfer })
  await waitFor(() => expect(reorder).toHaveBeenCalledWith(['Third', 'First', 'Second']))
  await waitFor(() =>
    expect(screen.getByRole('link', { name: 'Third' }).getAttribute('href')).toBe('/dashboards'),
  )
  expect(screen.getByTestId('url').textContent).toBe('?id=Second')
  fireEvent.click(screen.getByRole('link', { name: 'Third' }))
  expect(screen.getByTestId('url').textContent).toBe('')
  fireEvent.keyDown(screen.getByRole('button', { name: 'Reorder Third' }), { key: 'ArrowDown' })
  await waitFor(() => expect(reorder).toHaveBeenLastCalledWith(['First', 'Third', 'Second']))
})
