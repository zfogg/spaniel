// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Routes, Route, useLocation, useNavigate } from 'react-router-dom'
import { DashboardEditor } from './Dashboards'
import { api, type DashboardPanel } from '@/lib/api'

vi.mock('@/components/dashboard-panels/PanelRenderer', () => ({
  PanelRenderer: () => <div>Panel data</div>,
}))

function NavigationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <output data-testid="search">{location.search}</output>
      <button onClick={() => navigate(-1)}>Back</button>
    </>
  )
}

it.each(['panels', 'library', 'invalid'])(
  'reads the %s tab from the URL and preserves other parameters',
  async (initialTab) => {
    vi.spyOn(api.dashboards, 'list').mockResolvedValue({
      data: [
        {
          id: 'tabs',
          name: 'Tabs',
          description: '',
          panels: [],
          variables: [],
          created_at: 0,
          updated_at: 0,
        },
      ],
      meta: { total: 1 },
    })
    vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter initialEntries={[`/dashboards/tabs?other=keep&tab=${initialTab}`]}>
          <NavigationProbe />
          <Routes>
            <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    const initialName =
      initialTab === 'invalid' ? 'Design' : initialTab === 'panels' ? 'Panels' : 'Library'
    expect(
      (await screen.findByRole('tab', { name: initialName })).getAttribute('aria-selected'),
    ).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: 'Design' }))
    if (initialTab === 'invalid') fireEvent.click(screen.getByRole('tab', { name: 'Library' }))
    expect(screen.getByTestId('search').textContent).toBe(
      `?other=keep&tab=${initialTab === 'invalid' ? 'library' : 'design'}`,
    )
    fireEvent.click(screen.getByRole('button', { name: /^Back$/ }))
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: initialName }).getAttribute('aria-selected')).toBe(
        'true',
      ),
    )
  },
)

it('moves panels in both directions and omits unavailable boundary arrows', async () => {
  let panels: DashboardPanel[] = ['First', 'Middle', 'Last'].map((title, position) => ({
    id: title,
    dashboard_id: 'order',
    title,
    position,
    display_type: 'single_value',
    query_sql: 'SELECT 1',
    query_version: 1,
    settings_json: '{}',
    layout_json: '{}',
    updated_at: 0,
  }))
  const dashboard = () => ({
    id: 'order',
    name: 'Order',
    description: '',
    panels,
    variables: [],
    created_at: 0,
    updated_at: 0,
  })
  vi.spyOn(api.dashboards, 'list').mockImplementation(async () => ({
    data: [dashboard()],
    meta: { total: 1 },
  }))
  vi.spyOn(api.dashboards, 'get').mockImplementation(async () => ({
    data: dashboard(),
    meta: { total: 1 },
  }))
  vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({ data: [], meta: { total: 0 } })
  const move = vi
    .spyOn(api.dashboards, 'movePanel')
    .mockImplementation(async (_, id, direction) => {
      const next = [...panels],
        index = next.findIndex((panel) => panel.id === id)
      ;[next[index], next[index + direction]] = [next[index + direction], next[index]]
      panels = next.map((panel, position) => ({ ...panel, position }))
      return { data: { ok: true }, meta: { total: 1 } }
    })
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/dashboards/order']}>
        <Routes>
          <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: 'Panels' }))
  expect(screen.queryByRole('button', { name: 'Move First up' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Move Last down' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Move Middle up' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Move Middle up' })).toBeNull())
  expect(move).toHaveBeenCalledWith('order', 'Middle', -1)
  fireEvent.click(screen.getByRole('button', { name: 'Move First down' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Move First down' })).toBeNull())
  expect(move).toHaveBeenCalledWith('order', 'First', 1)
  expect(panels.map((panel) => panel.title)).toEqual(['Middle', 'Last', 'First'])
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('defaults to Design, preserves drafts across tabs, and creates the selected library panel', async () => {
  vi.spyOn(api.dashboards, 'list').mockResolvedValue({
    data: [
      {
        id: 'tabs',
        name: 'Tabs',
        description: '',
        panels: [],
        variables: [],
        created_at: 0,
        updated_at: 0,
      },
    ],
    meta: { total: 1 },
  })
  vi.spyOn(api.dashboards, 'catalog').mockResolvedValue({
    data: [
      {
        signal: 'spans',
        name: 'Span total',
        display_type: 'single_value',
        query: 'SELECT count(*) AS value FROM spans',
      },
    ],
    meta: { total: 1 },
  })
  const create = vi
    .spyOn(api.dashboards, 'panel')
    .mockResolvedValue({ data: {} as never, meta: { total: 1 } })
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/dashboards/tabs']}>
        <Routes>
          <Route path="/dashboards/:dashboardId" element={<DashboardEditor />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  const design = await screen.findByRole('tab', { name: 'Design' })
  expect(design.getAttribute('aria-selected')).toBe('true')
  expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
    'Design',
    'Library',
    'Panels',
  ])
  fireEvent.change(screen.getByRole('textbox', { name: 'Panel name' }), {
    target: { value: 'My draft' },
  })
  fireEvent.click(screen.getByRole('tab', { name: 'Library' }))
  expect(screen.queryByRole('button', { name: 'Run preview' })).toBeNull()
  expect(screen.queryByRole('button', { name: /^Create panel$/ })).toBeNull()
  fireEvent.click(design)
  expect((screen.getByRole('textbox', { name: 'Panel name' }) as HTMLInputElement).value).toBe(
    'My draft',
  )
  fireEvent.click(screen.getByRole('tab', { name: 'Library' }))
  fireEvent.click(await screen.findByRole('button', { name: /Span total/ }))
  fireEvent.click(screen.getByRole('button', { name: /^Create panel$/ }))
  expect(create).toHaveBeenCalledWith(
    'tabs',
    expect.objectContaining({
      title: 'Span total',
      display_type: 'single_value',
      query_sql: 'SELECT count(*) AS value FROM spans',
    }),
  )
  fireEvent.click(screen.getByRole('tab', { name: 'Panels' }))
  expect(screen.getByText('Add a panel to begin.')).toBeTruthy()
})
