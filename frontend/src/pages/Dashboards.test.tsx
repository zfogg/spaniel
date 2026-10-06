// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { DashboardEditor } from './Dashboards'

afterEach(() => { cleanup(); vi.restoreAllMocks(); sessionStorage.clear() })

describe('dashboard deletion', () => {
  for (const path of ['/dashboards/new', '/dashboards/test-dashboard']) {
    for (const confirmed of [false, true]) {
      it(`${confirmed ? 'deletes' : 'keeps'} the selected dashboard at ${path} after confirmation`, async () => {
        vi.spyOn(api.dashboards, 'list').mockResolvedValue({ data: [{ id: 'test-dashboard', name: 'Test dashboard', description: '', created_at: 1, updated_at: 1, panels: [], variables: [] }], meta: { total: 1 } })
        const remove = vi.spyOn(api.dashboards, 'remove').mockResolvedValue({ data: { ok: true }, meta: { total: 1 } })
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(confirmed)
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
        render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><Routes>
          <Route path="/dashboards/new" element={<DashboardEditor />} />
          <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
          <Route path="/dashboards" element={<p>Dashboard list destination</p>} />
        </Routes></MemoryRouter></QueryClientProvider>)
        await waitFor(() => expect((screen.getByLabelText('Dashboard name') as HTMLInputElement).value).toBe('Test dashboard'))
        fireEvent.click(screen.getByRole('button', { name: 'Delete dashboard' }))
        expect(confirm).toHaveBeenCalledWith('Delete “Test dashboard”? This cannot be undone.')
        if (confirmed) {
          await screen.findByText('Dashboard list destination')
          expect(remove).toHaveBeenCalledWith('test-dashboard')
        } else {
          expect(remove).not.toHaveBeenCalled()
          expect(screen.getByRole('button', { name: 'Delete dashboard' })).toBeTruthy()
        }
      })
    }
  }
})
