// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import TraceLogPanel from './TraceLogPanel'
import { api, type Log } from '@/lib/api'
import { logSelectionKey } from '@/lib/log-key'

vi.mock('@/lib/api', () => ({ api: { logs: { list: vi.fn() } } }))
const makeLog = (severity = 9, body = 'request completed'): Log => ({
  timestamp_ns: Date.now() * 1e6,
  received_at: 0,
  trace_id: 'trace-a',
  span_id: 'span-a',
  severity,
  body,
  attributes: '{}',
  service_name: 'api',
  session_id: 'archived-session',
})
const retry = vi.fn()
function show(
  logs: Log[] = [],
  total = logs.length,
  extra: Partial<React.ComponentProps<typeof TraceLogPanel>> = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <TraceLogPanel
          traceId="trace-a"
          sessionId="archived-session"
          logs={logs}
          total={total}
          loading={false}
          error={null}
          onRetry={retry}
          {...extra}
        />
      </BrowserRouter>
    </QueryClientProvider>,
  )
}
beforeEach(() => {
  window.history.replaceState({}, '', '/traces/trace-a?view=flame&span=span-a')
  vi.mocked(api.logs.list).mockResolvedValue({ data: [], meta: { total: 0 } })
})
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('defaults to a collapsed grey zero badge and preserves other URL state when toggled', async () => {
  show()
  expect(screen.getByTestId('trace-log-count').textContent).toBe('0 logs')
  expect(screen.getByTestId('trace-log-count').getAttribute('data-tone')).toBe('grey')
  expect(screen.queryByRole('region', { name: 'Trace logs' })).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: 'Expand trace logs' }))
  expect(new URLSearchParams(window.location.search).get('traceLogs')).toBe('open')
  expect(new URLSearchParams(window.location.search).get('view')).toBe('flame')
  expect(new URLSearchParams(window.location.search).get('span')).toBe('span-a')
  expect(screen.getByText('No logs in this trace')).toBeTruthy()
  await userEvent.click(screen.getByRole('button', { name: 'Collapse trace logs' }))
  expect(new URLSearchParams(window.location.search).has('traceLogs')).toBe(false)
  expect(screen.queryByRole('region', { name: 'Trace logs' })).toBeNull()
})

it.each([
  [12, false, false],
  [13, false, true],
  [16, false, true],
  [17, true, false],
  [20, true, false],
  [21, true, false],
])('uses OTLP severity bands for severity %i', (severity, error, warning) => {
  show([makeLog(severity)])
  expect(!!screen.queryByRole('img', { name: 'Error logs present' })).toBe(error)
  expect(!!screen.queryByRole('img', { name: 'Warning logs present' })).toBe(warning)
  expect(screen.getByTestId('trace-log-count').getAttribute('data-tone')).toBe('green')
})

it('shows both severity indicators and wraps only the URL-selected message', async () => {
  const warning = makeLog(13, 'a long warning '.repeat(80))
  const error = makeLog(17, 'a long error '.repeat(80))
  window.history.replaceState(
    {},
    '',
    `/traces/trace-a?view=waterfall&traceLogs=open&traceLog=${logSelectionKey(warning)}`,
  )
  show([warning, error])
  expect(screen.getByRole('img', { name: 'Error logs present' }).textContent).toBe('ERROR!')
  expect(screen.getByRole('img', { name: 'Warning logs present' }).textContent).toBe('WARNING!')
  const rows = screen.getAllByTestId('trace-log-row')
  expect(rows[0].getAttribute('data-selected')).toBe('true')
  expect(within(rows[0]).getByTestId('trace-log-message').className).toContain(
    'whitespace-pre-wrap',
  )
  expect(within(rows[1]).getByTestId('trace-log-message').className).toContain('truncate')
  await userEvent.click(screen.getByRole('button', { name: 'Expand log message 2' }))
  expect(new URLSearchParams(window.location.search).get('traceLog')).toBe(logSelectionKey(error))
  expect(rows[0].getAttribute('data-selected')).toBe('false')
  await userEvent.click(screen.getByRole('button', { name: 'Collapse log message 2' }))
  expect(new URLSearchParams(window.location.search).has('traceLog')).toBe(false)
})

it('checks the full trace for severity when the first page is incomplete', async () => {
  vi.mocked(api.logs.list).mockImplementation(async (params) => ({
    data:
      params?.severity === 'warn'
        ? [makeLog(13)]
        : params?.severity === 'error'
          ? [makeLog(17)]
          : [],
    meta: { total: params?.severity === 'fatal' ? 0 : 1 },
  }))
  show([makeLog(9)], 501)
  expect(screen.getByTestId('trace-log-count').textContent).toBe('501 logs')
  expect(await screen.findByRole('img', { name: 'Error logs present' })).toBeTruthy()
  expect(await screen.findByRole('img', { name: 'Warning logs present' })).toBeTruthy()
  expect(api.logs.list).toHaveBeenCalledWith({
    traceId: 'trace-a',
    sessionId: 'archived-session',
    severity: 'warn',
    page: 1,
    limit: 1,
  })
  expect(screen.queryByRole('region', { name: 'Trace logs' })).toBeNull()
})

it('restores the selected row and page from the URL and clears the selection when paging', async () => {
  const selected = makeLog(17, 'page two log')
  window.history.replaceState(
    {},
    '',
    `/traces/trace-a?traceLogs=open&traceLogsPage=2&traceLog=${logSelectionKey(selected)}`,
  )
  vi.mocked(api.logs.list).mockImplementation(async (params) => ({
    data: params?.page === 2 ? [selected] : [],
    meta: { total: params?.page === 2 ? 101 : 0 },
  }))
  show([makeLog()], 101)
  expect(await screen.findByRole('button', { name: 'Collapse log message 101' })).toBeTruthy()
  expect(api.logs.list).toHaveBeenCalledWith({
    traceId: 'trace-a',
    sessionId: 'archived-session',
    page: 2,
    limit: 100,
  })
  await userEvent.click(screen.getByRole('button', { name: 'Previous trace logs page' }))
  await waitFor(() =>
    expect(new URLSearchParams(window.location.search).has('traceLogsPage')).toBe(false),
  )
  expect(new URLSearchParams(window.location.search).has('traceLog')).toBe(false)
})

it('distinguishes loading and failure from zero logs and offers retry', async () => {
  show([], 0, { loading: true })
  expect(screen.getByTestId('trace-log-count').textContent).toBe('Loading logs…')
  cleanup()
  window.history.replaceState({}, '', '/traces/trace-a?traceLogs=open')
  show([], 0, { error: new Error('offline') })
  expect(screen.getByTestId('trace-log-count').textContent).toBe('Logs unavailable')
  expect(screen.getByRole('alert')).toBeTruthy()
  await userEvent.click(screen.getByRole('button', { name: 'Retry trace logs' }))
  expect(retry).toHaveBeenCalledOnce()
})

it('paginates prefetched logs without reducing the existing waterfall log window', () => {
  window.history.replaceState({}, '', '/traces/trace-a?traceLogs=open')
  const logs = Array.from({ length: 101 }, (_, i) => makeLog(i === 100 ? 13 : 9, `prefetched ${i}`))
  show(logs)
  expect(screen.getAllByTestId('trace-log-row')).toHaveLength(100)
  expect(screen.getByRole('img', { name: 'Warning logs present' })).toBeTruthy()
  expect(screen.getByTestId('trace-log-count').textContent).toBe('101 logs')
  expect(api.logs.list).not.toHaveBeenCalled()
})
