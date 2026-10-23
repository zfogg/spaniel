import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

const TRACE = '1234567890abcdef1234567890abcdef'
const SESSION = 'archived-session'
const LONG = 'Complete message with spaces\nand an unbroken token: ' + 'x'.repeat(1200)
function log(index: number, severity = 9, body = `log message ${index}`) {
  return {
    trace_id: TRACE,
    span_id: 'root',
    session_id: SESSION,
    service_name: 'orders',
    severity,
    body,
    timestamp_ns: (Date.now() - index) * 1e6,
    received_at: index,
    attributes: '{}',
  }
}
async function backend(page: Page, logs: ReturnType<typeof log>[] = []) {
  const state = { logs, fail: false, child: false, completed: false, event: false }
  const requests: URLSearchParams[] = []
  const sockets: WebSocketRoute[] = []
  await page.routeWebSocket('**/ws', (socket) => sockets.push(socket))
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const url = new URL(route.request().url())
      let data: unknown = []
      let total = 0
      if (url.pathname === `/api/traces/${TRACE}`)
        data = [
          {
            trace_id: TRACE,
            span_id: 'root',
            parent_span_id: '',
            service_name: 'orders',
            session_id: SESSION,
            session_label: SESSION,
            name: 'GET /orders',
            kind: 2,
            start_ns: Date.now() * 1e6,
            end_ns: (Date.now() + 100) * 1e6,
            duration_ns: 1e8,
            status_code: 1,
            attributes: '{}',
            resource: '{}',
          },
        ]
      if (url.pathname === `/api/traces/${TRACE}` && Array.isArray(data)) {
        if (state.completed)
          Object.assign(data[0], {
            name: 'completed orders',
            attributes: '{"live.attribute":"updated"}',
            end_ns: (Date.now() + 500) * 1e6,
          })
        if (state.child)
          data.push({
            ...(data[0] as object),
            span_id: 'child',
            parent_span_id: 'root',
            name: 'new live child',
          })
      }
      if (url.pathname === '/api/spans/root')
        data = {
          events: state.event
            ? [{ name: 'live span event', time_ns: Date.now() * 1e6, attributes: '{}' }]
            : [],
          links: [],
        }
      else if (url.pathname === '/api/sessions/active')
        data = { id: 'different-active-session', label: 'Live' }
      else if (url.pathname === '/api/stats')
        data = {
          span_count: 1,
          trace_count: 1,
          log_count: logs.length,
          db_size: 0,
          session_count: 1,
        }
      else if (url.pathname === '/api/logs') {
        requests.push(url.searchParams)
        if (state.fail) return route.fulfill({ status: 500, json: { error: 'logs offline' } })
        const severity = url.searchParams.get('severity')
        const matching = state.logs.filter(
          (row) =>
            row.trace_id === url.searchParams.get('traceId') &&
            row.session_id === url.searchParams.get('sessionId') &&
            (!severity ||
              (severity === 'error' && row.severity >= 17 && row.severity < 21) ||
              (severity === 'fatal' && row.severity >= 21) ||
              (severity === 'warn' && row.severity >= 13 && row.severity < 17)),
        )
        total = matching.length
        const pageNumber = Number(url.searchParams.get('page') ?? 1)
        const limit = Number(url.searchParams.get('limit') ?? 100)
        data = matching.slice((pageNumber - 1) * limit, pageNumber * limit)
      }
      await route.fulfill({
        json: { data, meta: { total, page: Number(url.searchParams.get('page') ?? 1) } },
      })
    },
  )
  return { state, requests, sockets }
}

test('empty trace logs are collapsed and grey; open and closed URL states survive refresh', async ({
  page,
}) => {
  const { requests } = await backend(page)
  await page.goto(`/traces/${TRACE}?view=waterfall&span=root`)
  const toggle = page.getByTestId('trace-log-toggle')
  await expect(page.getByTestId('trace-log-count')).toHaveText('0 logs')
  await expect(page.getByTestId('trace-log-count')).toHaveAttribute('data-tone', 'grey')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByRole('region', { name: 'Trace logs', exact: true })).toHaveCount(0)
  await toggle.click()
  await expect(page).toHaveURL(/traceLogs=open/)
  await expect(page.getByText('No logs in this trace', { exact: true })).toBeVisible()
  await page.reload()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  expect(new URL(page.url()).searchParams.get('view')).toBe('waterfall')
  expect(new URL(page.url()).searchParams.get('span')).toBe('root')
  expect(
    requests.every(
      (params) => params.get('traceId') === TRACE && params.get('sessionId') === SESSION,
    ),
  ).toBe(true)
  await toggle.click()
  await page.reload()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
})

test('overlay shows matching logs, both indicators, and URL-selected messages wrap after refresh', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 800 })
  await backend(page, [
    log(1, 9, LONG),
    log(2, 13),
    log(3, 17),
    log(4),
    log(5),
    { ...log(6), trace_id: 'unrelated' },
  ])
  await page.goto(`/traces/${TRACE}?view=waterfall`)
  const panel = page.getByTestId('trace-log-panel')
  await expect(page.getByTestId('trace-log-count')).toHaveText('5 logs')
  await expect(page.getByTestId('trace-log-count')).toHaveAttribute('data-tone', 'green')
  await expect(panel.getByRole('img', { name: 'Error logs present' })).toHaveText('ERROR!')
  await expect(panel.getByRole('img', { name: 'Warning logs present' })).toHaveText('WARNING!')
  const before = await panel.locator('..').boundingBox()
  await page.getByTestId('trace-log-toggle').click()
  await expect(panel).toHaveCSS('position', 'absolute')
  const after = await panel.locator('..').boundingBox()
  expect(after?.height).toBe(before?.height)
  const row = panel.getByTestId('trace-log-row').first()
  await expect(panel.getByTestId('trace-log-row')).toHaveCount(5)
  const compact = await row.boundingBox()
  await row.getByRole('button', { name: 'Expand log message 1', exact: true }).click()
  await expect(page).toHaveURL(/traceLog=[a-z0-9]+/)
  const message = row.getByTestId('trace-log-message')
  await expect(message).toHaveCSS('white-space', 'pre-wrap')
  await expect(message).toHaveCSS('overflow-wrap', 'anywhere')
  expect((await row.boundingBox())!.height).toBeGreaterThan(compact!.height + 40)
  expect(await message.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  const selectedUrl = page.url()
  await page.reload()
  await expect(row).toHaveAttribute('data-selected', 'true')
  await expect(message).toHaveCSS('white-space', 'pre-wrap')
  expect(page.url()).toBe(selectedUrl)
  await page.screenshot({ path: 'test-results/trace-log-panel.png' })
  await row.getByRole('button', { name: 'Collapse log message 1', exact: true }).click()
  await expect(message).toHaveCSS('text-overflow', 'ellipsis')
  expect(new URL(page.url()).searchParams.has('traceLog')).toBe(false)
})

test('counts and severity cover later pages; selected page and message restore from the URL', async ({
  page,
}) => {
  await backend(page, [
    ...Array.from({ length: 100 }, (_, i) => log(i)),
    log(100, 13),
    log(101, 17, LONG),
  ])
  await page.goto(`/traces/${TRACE}`)
  const panel = page.getByTestId('trace-log-panel')
  await expect(page.getByTestId('trace-log-count')).toHaveText('102 logs')
  await expect(panel.getByRole('img', { name: 'Error logs present' })).toBeVisible()
  await expect(panel.getByRole('img', { name: 'Warning logs present' })).toBeVisible()
  await page.getByTestId('trace-log-toggle').click()
  await expect(panel.getByTestId('trace-log-row')).toHaveCount(100)
  await panel.getByRole('button', { name: 'Next trace logs page' }).click()
  await expect(panel.getByTestId('trace-log-row')).toHaveCount(2)
  await panel.getByRole('button', { name: 'Expand log message 102', exact: true }).click()
  await page.reload()
  expect(new URL(page.url()).searchParams.get('traceLogsPage')).toBe('2')
  await expect(
    panel.getByRole('button', { name: 'Collapse log message 102', exact: true }),
  ).toBeVisible()
  await page.getByTestId('trace-log-toggle').click()
  await page.reload()
  await expect(page.getByTestId('trace-log-toggle')).toHaveAttribute('aria-expanded', 'false')
})

test('live matching logs update the count and indicators without including another trace', async ({
  page,
}) => {
  const { sockets } = await backend(page, [log(1)])
  await page.goto(`/traces/${TRACE}`)
  await expect(page.getByTestId('trace-log-count')).toHaveText('1 logs')
  await expect.poll(() => sockets.length).toBeGreaterThan(0)
  function send(row: ReturnType<typeof log>) {
    for (const socket of sockets)
      socket.send(
        JSON.stringify({
          type: 'log',
          timestamp_ns: row.timestamp_ns,
          payload: {
            row,
            traceId: row.trace_id,
            spanId: row.span_id,
            sessionId: row.session_id,
            serviceName: row.service_name,
            severity: row.severity,
            body: row.body,
          },
        }),
      )
  }
  send({ ...log(2, 17), trace_id: 'unrelated' })
  send(log(3, 13))
  await expect(page.getByTestId('trace-log-count')).toHaveText('2 logs')
  await expect(page.getByRole('img', { name: 'Warning logs present' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'Error logs present' })).toHaveCount(0)
  send(log(4, 17))
  await expect(page.getByTestId('trace-log-count')).toHaveText('3 logs')
  await expect(page.getByRole('img', { name: 'Error logs present' })).toBeVisible()
  await page.getByTestId('trace-log-toggle').click()
  await expect(page.getByTestId('trace-log-row')).toHaveCount(3)
})

test('a failed log query is not reported as zero and can be retried', async ({ page }) => {
  const { state } = await backend(page, [log(1)])
  state.fail = true
  await page.goto(`/traces/${TRACE}`)
  await expect(page.getByTestId('trace-log-count')).toHaveText('Logs unavailable')
  await page.getByTestId('trace-log-toggle').click()
  await expect(page.getByRole('alert')).toContainText('Couldn’t load trace logs.')
  state.fail = false
  await page.getByRole('button', { name: 'Retry trace logs' }).click()
  await expect(page.getByTestId('trace-log-count')).toHaveText('1 logs')
  await expect(page.getByTestId('trace-log-row')).toHaveCount(1)
})

test('trace metadata, spans, inspector attributes and events refresh while selection survives', async ({
  page,
}) => {
  const { state, sockets } = await backend(page)
  await page.goto(`/traces/${TRACE}?view=waterfall&span=root`)
  await expect(page.getByText('1 spans', { exact: true })).toBeVisible()
  state.completed = true
  state.child = true
  state.event = true
  for (const socket of sockets)
    socket.send(
      JSON.stringify({
        type: 'span',
        payload: { traceId: TRACE, spanId: 'child' },
        timestamp_ns: 0,
      }),
    )
  await expect(page.getByText('2 spans', { exact: true })).toBeVisible()
  await expect(page.getByText('new live child', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('treeitem', { name: 'live.attribute:"updated"', exact: true }),
  ).toBeVisible()
  await expect(page.getByTestId('span-events')).toContainText('live span event')
  expect(new URL(page.url()).searchParams.get('span')).toBe('root')
  expect(new URL(page.url()).searchParams.get('view')).toBe('waterfall')
})

test('rowless and self log events refresh the trace log panel without resetting its URL state', async ({
  page,
}) => {
  const { state, sockets } = await backend(page, [log(1)])
  await page.goto(`/traces/${TRACE}?traceLogs=open&view=waterfall`)
  await expect(page.getByTestId('trace-log-count')).toHaveText('1 logs')
  await page.getByRole('button', { name: 'Expand log message 1', exact: true }).click()
  const selection = new URL(page.url()).searchParams.get('traceLog')
  state.logs.push(log(2, 17))
  for (const socket of sockets)
    socket.send(
      JSON.stringify({ type: 'log', payload: { traceId: TRACE, spanId: 'root' }, timestamp_ns: 0 }),
    )
  await expect(page.getByTestId('trace-log-count')).toHaveText('2 logs')
  state.logs.push(log(3, 13))
  for (const socket of sockets)
    socket.send(JSON.stringify({ type: 'self_log', payload: state.logs[2], timestamp_ns: 0 }))
  await expect(page.getByTestId('trace-log-count')).toHaveText('3 logs')
  await expect(page.getByRole('img', { name: 'Warning logs present' })).toBeVisible()
  expect(new URL(page.url()).searchParams.get('traceLog')).toBe(selection)
  await expect(page.getByTestId('trace-log-toggle')).toHaveAttribute('aria-expanded', 'true')
})

test('live logs refresh later pages and retain pagination', async ({ page }) => {
  const { state, sockets } = await backend(
    page,
    Array.from({ length: 102 }, (_, i) => log(i + 1)),
  )
  await page.goto(`/traces/${TRACE}?traceLogs=open&traceLogsPage=2`)
  await expect(page.getByTestId('trace-log-row')).toHaveCount(2)
  state.logs.push(log(103, 17, 'arrived on page two'))
  for (const socket of sockets)
    socket.send(JSON.stringify({ type: 'log', payload: { traceId: TRACE }, timestamp_ns: 0 }))
  await expect(page.getByTestId('trace-log-row')).toHaveCount(3)
  await expect(page.getByText('arrived on page two', { exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.get('traceLogsPage')).toBe('2')
})
