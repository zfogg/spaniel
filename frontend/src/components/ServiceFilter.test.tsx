// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router-dom'
import ServiceFilter, { type TelemetrySignal } from './ServiceFilter'
import { useServiceSelection } from '@/lib/use-service-selection'
import { api } from '@/lib/api'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

function Harness({ signal }: { signal: TelemetrySignal }) {
  const [selected, setSelected] = useServiceSelection()
  const location = useLocation()
  return (
    <>
      <ServiceFilter signal={signal} selected={selected} onChange={setSelected} />
      <output>{location.search}</output>
    </>
  )
}

it.each(['traces', 'spans', 'logs', 'metrics'] as const)(
  'discovers all %s services independently and selects a union',
  async (signal) => {
    const list = vi.spyOn(api.services, 'list').mockResolvedValue({
      data: ['busy', 'old-worker', 'logs,metrics'],
      meta: { total: 3, page: 1 },
    })
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter initialEntries={['/?session=session-a']}>
          <Harness signal={signal} />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await screen.findByRole('checkbox', { name: 'old-worker' })
    expect(list).toHaveBeenCalledWith({ signal, sessionId: 'session-a' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'old-worker' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'logs,metrics' }))
    await waitFor(() => {
      expect(
        (screen.getByRole('checkbox', { name: 'old-worker' }) as HTMLInputElement).checked,
      ).toBe(true)
      expect(
        (screen.getByRole('checkbox', { name: 'logs,metrics' }) as HTMLInputElement).checked,
      ).toBe(true)
    })
    expect(screen.getByRole('status').textContent).toContain(
      'services=old-worker&services=logs%2Cmetrics',
    )
    expect(screen.getAllByRole('checkbox')).toHaveLength(3)
    fireEvent.click(screen.getByRole('button', { name: 'All services' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('?session=session-a'))
  },
)
