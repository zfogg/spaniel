import { expect, test, type Page } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  page.on('pageerror', (error) => console.error('Dashboard browser error:', error.message))
})

async function visibility(page: Page, state: DocumentVisibilityState) {
  await page.evaluate((value) => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value })
    document.dispatchEvent(new Event('visibilitychange'))
  }, state)
}

test('dashboard panels query once, pause while hidden, and resume without resetting variables', async ({
  page,
}) => {
  await page.clock.install()
  const panels: { variables: Record<string, string> }[] = []
  const states: boolean[] = []
  let send: ((event: object) => void) | undefined
  let unusedMetadataReads = 0
  const dashboard = {
    id: 'visible-dashboard',
    name: 'Viewer workload',
    description: '',
    created_at: 1,
    updated_at: 1,
    variables: [
      {
        dashboard_id: 'visible-dashboard',
        name: 'service',
        kind: 'string',
        source: '',
        options_json: '[]',
        default_value: 'orders',
      },
    ],
    panels: [
      {
        id: 'count',
        dashboard_id: 'visible-dashboard',
        title: 'Visible result',
        display_type: 'single_value',
        query_sql:
          'SELECT count(*) AS value FROM spans WHERE session_id=$session_id AND service_name=$service',
        query_version: 1,
        settings_json: '{}',
        layout_json: '{}',
        position: 0,
      },
    ],
  }
  await page.routeWebSocket('**/ws', (socket) => {
    send = (event) => socket.send(JSON.stringify(event))
    socket.onMessage((message) => {
      const frame = JSON.parse(String(message))
      if (frame.type === 'visibility') states.push(frame.visible)
    })
  })
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/dashboards') data = [dashboard]
    else if (path === '/api/dashboards/visible-dashboard') {
      unusedMetadataReads++
      data = dashboard
    } else if (path.endsWith('/query-preview')) {
      panels.push(route.request().postDataJSON())
      data = { columns: ['value'], rows: [{ value: 42 }], truncated: false, query_version: 1 }
    } else if (path === '/api/sessions/active') data = { id: 'session', label: 'Session' }
    else if (path === '/api/stats')
      data = { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 1 }
    await route.fulfill({ json: { data } })
  })
  await page.goto('/dashboards')
  await expect(page.getByRole('heading', { name: 'Visible result' })).toBeVisible()
  await expect(page.getByText('42', { exact: true }).first()).toBeVisible()
  expect(panels).toHaveLength(1)
  expect(panels[0].variables).toEqual({ service: 'orders' })
  expect(unusedMetadataReads).toBe(0)
  await page.getByLabel('Dashboard variable service').fill('payments')
  await page.clock.runFor(50)
  await expect.poll(() => panels.at(-1)?.variables.service).toBe('payments')
  const before = panels.length
  await visibility(page, 'hidden')
  await expect.poll(() => states.at(-1)).toBe(false)
  for (let i = 0; i < 7; i++) {
    send?.({ type: 'span', payload: {}, timestamp_ns: i })
    send?.({ type: 'heartbeat', timestamp_ns: i })
    await page.clock.runFor(5000)
  }
  expect(panels).toHaveLength(before)
  await visibility(page, 'visible')
  await expect.poll(() => states.at(-1)).toBe(true)
  await expect.poll(() => panels.length).toBe(before + 1)
  await expect(page.getByLabel('Dashboard variable service')).toHaveValue('payments')
  await page.clock.runFor(100)
  expect(panels).toHaveLength(before + 1)
  await page.getByRole('button', { name: 'Telemetry', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Logs', exact: true }).click()
  const afterLeaving = panels.length
  send?.({ type: 'span', payload: {}, timestamp_ns: 99 })
  await page.clock.runFor(35_000)
  expect(panels).toHaveLength(afterLeaving)
})

test('a dashboard opened in a hidden tab waits for its first viewer', async ({ page }) => {
  let dashboardReads = 0
  await page.addInitScript(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
  })
  await page.routeWebSocket('**/ws', () => {})
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/dashboards') dashboardReads++
    await route.fulfill({
      json: { data: path === '/api/sessions/active' ? { id: 'session', label: 'Session' } : [] },
    })
  })
  await page.goto('/dashboards')
  await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible()
  expect(dashboardReads).toBe(0)
  await visibility(page, 'visible')
  await expect.poll(() => dashboardReads).toBe(1)
})

test('metric refreshes keep the chosen operation and ignore unrelated streams and hidden tabs', async ({
  page,
}) => {
  await page.clock.install()
  const operations: string[] = []
  let catalogReads = 0
  let send: ((event: object) => void) | undefined
  await page.routeWebSocket('**/ws', (socket) => {
    send = (event) => socket.send(JSON.stringify(event))
  })
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const url = new URL(route.request().url())
    let data: unknown = []
    if (url.pathname === '/api/metrics') {
      catalogReads++
      data = [
        {
          name: 'http.dur',
          service_name: 'api',
          type: 'histogram',
          unit: 'ms',
          description: 'Duration',
          sample_count: catalogReads,
          last_timestamp_ns: 1,
        },
      ]
    } else if (url.pathname === '/api/metrics/series') {
      operations.push(url.searchParams.get('operation') ?? 'raw')
      data = {
        name: 'http.dur',
        service_name: 'api',
        type: 'histogram',
        unit: 'ms',
        description: 'Duration',
        points: [
          {
            timestamp_ns: 1_000_000_000,
            value: 9,
            count: 1,
            sum: 9,
            bounds: [10],
            buckets: [1, 0],
          },
        ],
        traces: [],
      }
    } else if (url.pathname === '/api/sessions/active') data = { id: 'session', label: 'Session' }
    await route.fulfill({ json: { data } })
  })
  await page.goto('/metrics?metric=http.dur&service=api')
  await expect(page.getByRole('heading', { name: 'http.dur', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'p90', exact: true }).click()
  await expect.poll(() => operations.at(-1)).toBe('p90')
  const before = operations.length
  const catalogBefore = catalogReads
  send?.({
    type: 'metric',
    timestamp_ns: 0,
    payload: { name: 'unrelated', serviceName: 'other', value: 1, type: 'gauge' },
  })
  await page.clock.runFor(1200)
  await expect.poll(() => catalogReads).toBeGreaterThan(catalogBefore)
  await page.clock.runFor(100)
  expect(operations).toHaveLength(before)
  await visibility(page, 'hidden')
  send?.({
    type: 'metric',
    timestamp_ns: 1,
    payload: { name: 'http.dur', serviceName: 'api', value: 1, type: 'histogram' },
  })
  await page.clock.runFor(3000)
  expect(operations).toHaveLength(before)
  await visibility(page, 'visible')
  await expect.poll(() => operations.length).toBe(before + 1)
  expect(operations.at(-1)).toBe('p90')
})
