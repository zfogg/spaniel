import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function json(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

const TRACE_ID = 'abc123'
const ZERO = '0000000000000000'

function span(o: {
  span_id: string
  parent_span_id?: string
  service_name?: string
  name?: string
  start_ns?: number
  duration_ns?: number
  status_code?: number
  attributes?: Record<string, unknown>
  resource?: Record<string, unknown>
}) {
  const start = o.start_ns ?? 0
  const dur = o.duration_ns ?? 1_000_000
  return {
    trace_id: TRACE_ID,
    span_id: o.span_id,
    parent_span_id: o.parent_span_id ?? ZERO,
    service_name: o.service_name ?? 'api',
    name: o.name ?? 'op',
    kind: 1,
    start_ns: start,
    end_ns: start + dur,
    duration_ns: dur,
    status_code: o.status_code ?? 1,
    status_message: '',
    attributes: JSON.stringify(o.attributes ?? {}),
    resource: JSON.stringify(o.resource ?? {}),
    session_id: 's1',
    session_label: 'live',
    received_at: 0,
  }
}

interface Fixture {
  spans?: ReturnType<typeof span>[]
  warnings?: Array<{
    span_id: string
    trace_id: string
    session_id: string
    rule_id: string
    message: string
    severity: string
    created_at: number
  }>
  issues?: Array<{
    id: string
    trace_id: string
    session_id: string
    kind: string
    fingerprint: string
    count: number
    wasted_ns: number
    parent_span_id: string
    example_span_id: string
    created_at: number
  }>
  logs?: Array<{
    timestamp_ns: number
    trace_id: string
    span_id: string
    severity: number
    body: string
    attributes: string
    service_name: string
    session_id: string
    received_at: number
  }>
}

async function stubBackend(page: Page, fx: Fixture) {
  await page.routeWebSocket('**/ws', ws => ws.close())

  // Single predicate handler for all API routes — avoids LIFO ordering races
  // that can occur when many glob patterns are registered in parallel workers.
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const { pathname, searchParams } = new URL(r.request().url())

    if (pathname === '/api/stats')
      return json(r, { span_count: fx.spans?.length ?? 0, trace_count: 1, log_count: fx.logs?.length ?? 0, db_size: 0, session_count: 1, oldest_session_at: 0 })
    if (pathname === '/api/forwarders')
      return json(r, [])
    if (pathname === '/api/sessions/active')
      return json(r, { id: 's1', label: 'live' })
    if (pathname === '/api/sessions')
      return json(r, [])
    if (pathname === '/api/services')
      return json(r, ['api'])

    // Trace detail — must be checked before the list fallback.
    if (pathname === `/api/traces/${TRACE_ID}`)
      return json(r, fx.spans ?? [])
    if (pathname === '/api/traces')
      return json(r, [])

    if (pathname.startsWith('/api/lint'))
      return json(r, fx.warnings ?? [])
    if (pathname.startsWith('/api/issues'))
      return json(r, fx.issues ?? [])
    if (pathname.startsWith('/api/logs')) {
      const spanId = searchParams.get('spanId')
      return json(r, (fx.logs ?? []).filter(l => !spanId || l.span_id === spanId))
    }

    return r.continue()
  })
}

// ── shared fixture: 5-span trace, 100ms total ────────────────────────────────

function makeTrace(): Required<Pick<Fixture, 'spans'>> {
  return {
    spans: [
      span({ span_id: 'root', name: 'GET /cart', service_name: 'api', start_ns: 0, duration_ns: 100_000_000,
        attributes: { 'http.request.method': 'GET', 'http.route': '/cart' } }),
      span({ span_id: 'auth', parent_span_id: 'root', name: 'authenticate', service_name: 'api',
        start_ns: 5_000_000, duration_ns: 10_000_000 }),
      span({ span_id: 'db1', parent_span_id: 'root', name: 'SELECT items', service_name: 'postgres',
        start_ns: 20_000_000, duration_ns: 30_000_000,
        attributes: { 'db.system': 'postgresql', 'db.statement': 'SELECT * FROM items' } }),
      span({ span_id: 'db2', parent_span_id: 'root', name: 'SELECT promos', service_name: 'postgres',
        start_ns: 55_000_000, duration_ns: 40_000_000,
        attributes: { 'db.system': 'postgresql', 'db.statement': 'SELECT * FROM promos' } }),
      span({ span_id: 'render', parent_span_id: 'root', name: 'render', service_name: 'api',
        start_ns: 95_000_000, duration_ns: 4_000_000 }),
    ],
  }
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Trace detail page', () => {
  test('renders the waterfall with header, span rows, and root name', async ({ page }) => {
    await stubBackend(page, makeTrace())
    await page.goto(`/traces/${TRACE_ID}`)

    // Loading state clears.
    await expect(page.getByText('Loading…')).toHaveCount(0, { timeout: 5_000 })

    // Root span name is in the trace meta header.
    await expect(page.getByText('GET /cart').first()).toBeVisible()

    // All 5 stubbed spans are visible as rows (header row uses the same text
    // only for root, so the others appear exactly once each).
    await expect(page.getByText('authenticate', { exact: true })).toBeVisible()
    await expect(page.getByText('SELECT items', { exact: true })).toBeVisible()
    await expect(page.getByText('SELECT promos', { exact: true })).toBeVisible()
    await expect(page.getByText('render', { exact: true })).toBeVisible()
  })

  test('renders the timing ruler with zero and total-duration tick labels', async ({ page }) => {
    await stubBackend(page, makeTrace())
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('GET /cart').first()).toBeVisible()

    // Ruler ticks: 0 doesn't render a label (i===0 is empty), but mid + end do.
    // 100ms trace → ticks at 0, ~16, ~33, 50, ~66, ~83. Just assert one mid-tick.
    await expect(page.locator('text=/50\\.0ms/').first()).toBeVisible()
  })

  test('clicking a span row opens the inspector with attrs', async ({ page }) => {
    await stubBackend(page, makeTrace())
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('SELECT items', { exact: true })).toBeVisible()

    await page.getByText('SELECT items', { exact: true }).click()

    // Inspector shows the selected span: service name and the duration of 30ms.
    await expect(page.getByText('postgres').first()).toBeVisible()
    await expect(page.getByText(/30\.0ms/).first()).toBeVisible()

    // Attribute key from the stubbed attributes is visible (attrs tab is default).
    await expect(page.getByText('db.statement').first()).toBeVisible()
  })

  test('shows the n+1 badge on the span flagged by a server issue', async ({ page }) => {
    const { spans } = makeTrace()
    await stubBackend(page, {
      spans,
      issues: [{
        id: 'iss-1', trace_id: TRACE_ID, session_id: 's1', kind: 'n_plus_one',
        fingerprint: 'SELECT * FROM items', count: 12, wasted_ns: 20_000_000,
        parent_span_id: 'root', example_span_id: 'db1', created_at: 0,
      }],
    })
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('SELECT items', { exact: true })).toBeVisible()

    // Lower-case "n+1" badge on the row (TraceWaterfall renders the literal n+1 chip).
    await expect(page.getByText('n+1', { exact: true }).first()).toBeVisible()
  })

  test('lint warnings appear in the inspector for the affected span', async ({ page }) => {
    const { spans } = makeTrace()
    await stubBackend(page, {
      spans,
      warnings: [{
        span_id: 'auth', trace_id: TRACE_ID, session_id: 's1',
        rule_id: 'http.missing_status_code',
        message: 'HTTP span is missing http.response.status_code',
        severity: 'warn', created_at: 0,
      }],
    })
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('authenticate', { exact: true })).toBeVisible()

    // Top-of-page meta shows the lint count.
    await expect(page.getByText(/1 lint/i)).toBeVisible()

    // Select the warned span → its inspector shows the rule id + message.
    await page.getByText('authenticate', { exact: true }).click()
    await expect(page.getByText('http.missing_status_code')).toBeVisible()
    await expect(page.getByText(/missing http\.response\.status_code/)).toBeVisible()
  })

  test('switching to the graph tab renders SVG nodes', async ({ page }) => {
    await stubBackend(page, makeTrace())
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('GET /cart').first()).toBeVisible()

    await page.getByRole('button', { name: /graph/i }).click()

    // ReactFlow / dagre render <svg> elements for the DAG.
    await expect(page.locator('svg').first()).toBeVisible()
  })

  test('selected span shows correlated logs in the logs tab', async ({ page }) => {
    const { spans } = makeTrace()
    await stubBackend(page, {
      spans,
      logs: [{
        timestamp_ns: 25_000_000, trace_id: TRACE_ID, span_id: 'db1',
        severity: 9, body: 'slow query: SELECT * FROM items',
        attributes: '{}', service_name: 'postgres',
        session_id: 's1', received_at: 0,
      }],
    })
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page.getByText('SELECT items', { exact: true })).toBeVisible()

    await page.getByText('SELECT items', { exact: true }).click()
    await page.getByRole('button', { name: 'logs', exact: true }).click()

    await expect(page.getByText('slow query: SELECT * FROM items')).toBeVisible()
  })

  test('?spanId= deep-link pre-selects the span on load', async ({ page }) => {
    await stubBackend(page, makeTrace())
    await page.goto(`/traces/${TRACE_ID}?spanId=db1`)
    // Inspector for db1 (SELECT items) appears without any user click.
    await expect(page.getByText('postgres').first()).toBeVisible()
    await expect(page.getByText('db.statement').first()).toBeVisible()
  })

  test('back button navigates away from the trace detail page', async ({ page }) => {
    // Land on the traces index first so there's history to go back to.
    await stubBackend(page, makeTrace())
    await page.goto('/')
    await page.goto(`/traces/${TRACE_ID}`)
    await expect(page).toHaveURL(new RegExp(`/traces/${TRACE_ID}$`))

    await page.getByRole('button', { name: '←' }).click()
    await expect(page).not.toHaveURL(new RegExp(`/traces/${TRACE_ID}$`))
  })
})
