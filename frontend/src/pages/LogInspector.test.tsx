// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { LogInspector, LogRow } from './LogViewer'
import { api, type Log } from '@/lib/api'
import { qk } from '@/lib/query'
import { formatTimestamp } from '@/components/shared/time-format'

vi.mock('@/lib/api', () => ({ api: { traces: { get: vi.fn() } } }))
const log: Log = {
  attributes: '{}',
  body: 'request failed',
  received_at: (Date.now() - 10000) * 1e6,
  timestamp_ns: (Date.now() - 10000) * 1e6,
  severity: 17,
  service_name: 'ratio-api',
  session_id: 'session',
  trace_id: '1234567890abcdef1234567890abcdef',
  span_id: '1234567890abcdef',
}
beforeEach(() => {
  vi.mocked(api.traces.get).mockResolvedValue({ data: [{}] } as never)
})
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})
function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}
function inspect(value: Log) {
  render(<LogInspector log={value} onClose={() => {}} navigate={() => {}} />, { wrapper })
  return screen.getByRole('complementary', { name: 'Log details' })
}
it('links an available trace and selects the associated span', async () => {
  const panel = inspect(log)
  expect(
    (await within(panel).findByRole('link', { name: log.trace_id })).getAttribute('href'),
  ).toBe(`/traces/${log.trace_id}?spanId=${log.span_id}`)
  expect(within(panel).getAllByText(formatTimestamp(log.timestamp_ns)).length).toBeGreaterThan(0)
  expect(within(panel).getAllByText('moments ago').length).toBeGreaterThan(0)
  expect(within(panel).getByText(log.body).className).toContain('text-foreground')
})
it('does not invent a trace for uncorrelated records', () => {
  const panel = inspect({ ...log, trace_id: '0'.repeat(32), span_id: '' })
  expect(within(panel).queryByRole('link')).toBeNull()
  expect(within(panel).getByText('Not recorded')).toBeTruthy()
  expect(api.traces.get).not.toHaveBeenCalled()
})
it('omits zero span IDs from trace links', async () => {
  const panel = inspect({ ...log, span_id: '0'.repeat(16) })
  expect(
    (await within(panel).findByRole('link', { name: log.trace_id })).getAttribute('href'),
  ).toBe(`/traces/${log.trace_id}`)
})
it('marks missing spans red without offering broken navigation', async () => {
  vi.mocked(api.traces.get).mockResolvedValue({ data: [] } as never)
  const panel = inspect(log)
  expect((await within(panel).findByText('Trace not found')).className).toContain(
    'text-[var(--danger)]',
  )
  expect(within(panel).queryByRole('link')).toBeNull()
  expect(within(panel).queryByRole('button', { name: /Open trace/ })).toBeNull()
})
it('enables a trace link when late spans arrive', async () => {
  vi.mocked(api.traces.get).mockResolvedValue({ data: [] } as never)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <LogInspector log={log} onClose={() => {}} navigate={() => {}} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  await screen.findByText('Trace not found')
  client.setQueryData(qk.trace(log.trace_id), [{}])
  expect(await screen.findByRole('link', { name: log.trace_id })).toBeTruthy()
})
it.each([true, false])('checks trace availability before row navigation: %s', async (available) => {
  vi.mocked(api.traces.get).mockResolvedValue({ data: available ? [{}] : [] } as never)
  const navigate = vi.fn(),
    onSelect = vi.fn()
  render(<LogRow log={log} i={0} selected={false} navigate={navigate} onSelect={onSelect} />, {
    wrapper,
  })
  screen.getByTitle(`view trace ${log.trace_id}`).click()
  await waitFor(() => expect(available ? navigate : onSelect).toHaveBeenCalled())
  expect(available ? onSelect : navigate).not.toHaveBeenCalled()
})
