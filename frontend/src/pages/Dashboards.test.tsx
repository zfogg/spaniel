// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { DashboardEditor } from './Dashboards'

vi.mock('@/components/SqlCode', () => ({
  SqlCode: ({ value }: { value: string }) => <code>{value}</code>,
  SqlEditor: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <textarea
      aria-label="SQL editor"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  sessionStorage.clear()
})

describe('dashboard deletion', () => {
  for (const path of ['/dashboards/test-dashboard']) {
    for (const confirmed of [false, true]) {
      it(`${confirmed ? 'deletes' : 'keeps'} the selected dashboard at ${path} after confirmation`, async () => {
        vi.spyOn(api.dashboards, 'list').mockResolvedValue({
          data: [
            {
              id: 'test-dashboard',
              name: 'Test dashboard',
              description: '',
              created_at: 1,
              updated_at: 1,
              panels: [],
              variables: [],
            },
          ],
          meta: { total: 1 },
        })
        const remove = vi
          .spyOn(api.dashboards, 'remove')
          .mockResolvedValue({ data: { ok: true }, meta: { total: 1 } })
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(confirmed)
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
        render(
          <QueryClientProvider client={client}>
            <MemoryRouter initialEntries={[path]}>
              <Routes>
                <Route path="/dashboards/new" element={<DashboardEditor />} />
                <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
                <Route path="/dashboards" element={<p>Dashboard list destination</p>} />
              </Routes>
            </MemoryRouter>
          </QueryClientProvider>,
        )
        await waitFor(() =>
          expect((screen.getByLabelText('Dashboard name') as HTMLInputElement).value).toBe(
            'Test dashboard',
          ),
        )
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

describe('dashboard SQL preview', () => {
  it('runs the unsaved query and shows its typed result', async () => {
    vi.spyOn(api.dashboards, 'list').mockResolvedValue({
      data: [
        {
          id: 'test-dashboard',
          name: 'Test dashboard',
          description: '',
          created_at: 1,
          updated_at: 1,
          panels: [],
          variables: [],
        },
      ],
      meta: { total: 1 },
    })
    vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
    const preview = vi.spyOn(api.dashboards, 'preview').mockResolvedValue({
      data: { columns: ['value'], rows: [{ value: 42 }], warnings: [] },
      meta: { total: 1 },
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/dashboards/test-dashboard']}>
          <Routes>
            <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await screen.findByRole('button', { name: /Single value/ })
    fireEvent.click(screen.getByRole('button', { name: /Single value/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Run preview' }))
    await screen.findByText('42')
    expect(preview).toHaveBeenCalledWith(
      'test-dashboard',
      expect.objectContaining({ display_type: 'single_value' }),
    )
  })
})

describe('dashboard YAML config', () => {
  it('opens the exported syntax-highlighted text configuration', async () => {
    vi.spyOn(api.dashboards, 'list').mockResolvedValue({
      data: [
        {
          id: 'test-dashboard',
          name: 'Test dashboard',
          description: '',
          created_at: 1,
          updated_at: 1,
          panels: [],
          variables: [],
        },
      ],
      meta: { total: 1 },
    })
    vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
    const config = vi
      .spyOn(api.dashboards, 'config')
      .mockResolvedValue('version: 1\nname: Test dashboard\n')
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/dashboards/test-dashboard']}>
          <Routes>
            <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await screen.findByRole('button', { name: 'View YAML' })
    fireEvent.click(screen.getByRole('button', { name: 'View YAML' }))
    expect(
      (await screen.findByRole('dialog', { name: 'Dashboard YAML configuration' })).textContent,
    ).toContain('version: 1')
    expect(config).toHaveBeenCalledWith('test-dashboard')
  })

  it('imports a YAML definition from the new dashboard page', async () => {
    vi.spyOn(api.dashboards, 'list').mockResolvedValue({ data: [], meta: { total: 0 } })
    vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
    const imported = {
      id: 'imported-dashboard',
      name: 'Imported',
      description: '',
      created_at: 1,
      updated_at: 1,
      panels: [],
      variables: [],
    }
    const importConfig = vi
      .spyOn(api.dashboards, 'importConfig')
      .mockResolvedValue({ data: imported, meta: { total: 1 } })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/dashboards/new']}>
          <Routes>
            <Route path="/dashboards/new" element={<DashboardEditor />} />
            <Route path="/dashboards/:dashboardId" element={<p>Imported dashboard</p>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Import dashboard from YAML' }))
    expect(screen.getByRole('dialog', { name: 'Import dashboard from YAML' })).toBeTruthy()
    fireEvent.change(screen.getByRole('textbox', { name: 'Dashboard YAML' }), {
      target: { value: 'version: 1\nname: Imported\npanels: []' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Import dashboard' }))
    await screen.findByText('Imported dashboard')
    expect(importConfig).toHaveBeenCalledWith('version: 1\nname: Imported\npanels: []')
  })
})
