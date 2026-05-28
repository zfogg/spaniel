import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ───────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

function makeSpan(overrides: Record<string, unknown> = {}) {
  return {
    span_id: 'span-1',
    trace_id: 'trace-abc',
    parent_span_id: '',
    service_name: 'api-gateway',
    name: 'GET /api/users',
    kind: 2, // server
    start_ns: 1_700_000_000_000_000_000,
    end_ns:   1_700_000_000_100_000_000,
    duration_ns: 100_000_000, // 100ms
    status_code: 0,
    status_message: '',
    attributes: '{"http.method":"GET","http.status_code":200}',
    resource: '{}',
    session_id: 'sess-1',
    session_label: 'sess-1',
    received_at: 1_700_000_000_000_000_000,
    tag: null,
    ...overrides,
  }
}

async function stubBackend(page: Page, spans: unknown[] = []) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const pathname = new URL(r.request().url()).pathname
    if (pathname === '/api/stats')
      return jsonResponse(r, { span_count: spans.length, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 })
    if (pathname === '/api/forwarders') return jsonResponse(r, [])
    if (pathname === '/api/sessions/active') return jsonResponse(r, { id: '', label: '' })
    if (pathname.startsWith('/api/spans')) return jsonResponse(r, spans, { total: spans.length, page: 1 })
    return r.continue()
  })
}

// ── navigation ────────────────────────────────────────────────────────────────

test.describe('Spans page', () => {
  test('is accessible via the Spans nav link', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.getByRole('link', { name: 'Spans' }).click()
    await expect(page).toHaveURL('/spans')
  })

  test('shows empty state when no spans', async ({ page }) => {
    await stubBackend(page, [])
    await page.goto('/spans')
    await expect(page.getByText('No spans recorded')).toBeVisible()
  })

  // ── table rows ────────────────────────────────────────────────────────────

  test('renders span rows with name, service, kind, duration, and trace', async ({ page }) => {
    const span = makeSpan()
    await stubBackend(page, [span])
    await page.goto('/spans')

    const row = page.getByTestId('span-row-span-1')
    await expect(row.getByText('GET /api/users')).toBeVisible()
    await expect(row.getByText('api-gateway')).toBeVisible()
    await expect(row.getByText('SERVER')).toBeVisible()
    await expect(row.getByText('100.0ms')).toBeVisible()
    await expect(row.getByText('trace-abc')).toBeVisible()
  })

  test('shows n+1 tag badge for n+1 spans', async ({ page }) => {
    const span = makeSpan({ tag: 'n+1' })
    await stubBackend(page, [span])
    await page.goto('/spans')
    await expect(page.getByTestId('span-row-span-1').getByText('n+1')).toBeVisible()
  })

  test('shows slow tag badge and red duration for slow spans', async ({ page }) => {
    const span = makeSpan({ duration_ns: 300_000_000, tag: 'slow' })
    await stubBackend(page, [span])
    await page.goto('/spans')
    const row = page.getByTestId('span-row-span-1')
    await expect(row.getByText('slow')).toBeVisible()
    await expect(row.getByText('300.0ms')).toBeVisible()
  })

  test('shows lint tag badge', async ({ page }) => {
    const span = makeSpan({ tag: 'lint' })
    await stubBackend(page, [span])
    await page.goto('/spans')
    await expect(page.getByTestId('span-row-span-1').getByText('lint')).toBeVisible()
  })

  test('shows error tag badge', async ({ page }) => {
    const span = makeSpan({ tag: 'error', status_code: 2 })
    await stubBackend(page, [span])
    await page.goto('/spans')
    await expect(page.getByTestId('span-row-span-1').getByText('error')).toBeVisible()
  })

  // ── search filtering ──────────────────────────────────────────────────────

  test('filters rows by name search', async ({ page }) => {
    const spans = [
      makeSpan({ span_id: 'sp-1', name: 'GET /api/users' }),
      makeSpan({ span_id: 'sp-2', name: 'POST /api/orders' }),
    ]
    await stubBackend(page, spans)
    await page.goto('/spans')

    await page.getByTestId('spans-search').fill('orders')
    await expect(page.getByText('POST /api/orders')).toBeVisible()
    await expect(page.getByText('GET /api/users')).not.toBeVisible()
    await expect(page.getByText('1 of 2 spans')).toBeVisible()
  })

  test('clear button resets search', async ({ page }) => {
    const span = makeSpan()
    await stubBackend(page, [span])
    await page.goto('/spans')

    await page.getByTestId('spans-search').fill('xyz')
    await expect(page.getByText('no spans match')).toBeVisible()
    await page.getByRole('button', { name: 'clear' }).click()
    await expect(page.getByText('GET /api/users')).toBeVisible()
  })

  // ── sidebar facets ────────────────────────────────────────────────────────

  test('service facet filters visible rows', async ({ page }) => {
    const spans = [
      makeSpan({ span_id: 'sp-1', service_name: 'api-gateway', name: 'GET /api/users' }),
      makeSpan({ span_id: 'sp-2', service_name: 'auth-service', name: 'login' }),
    ]
    await stubBackend(page, spans)
    await page.goto('/spans')

    // click in sidebar (not row button)
    await page.getByTestId('spans-sidebar').getByRole('button', { name: /auth-service/ }).click()
    await expect(page.getByText('login')).toBeVisible()
    await expect(page.getByText('GET /api/users')).not.toBeVisible()

    // filter chip appears
    await expect(page.getByText('svc: auth-service')).toBeVisible()
  })

  test('kind facet filters visible rows', async ({ page }) => {
    const spans = [
      makeSpan({ span_id: 'sp-1', kind: 2, name: 'server-span' }),  // server
      makeSpan({ span_id: 'sp-2', kind: 3, name: 'client-span' }),  // client
    ]
    await stubBackend(page, spans)
    await page.goto('/spans')

    await page.getByTestId('spans-sidebar').getByRole('button', { name: /^client/ }).click()
    await expect(page.getByText('client-span')).toBeVisible()
    await expect(page.getByText('server-span')).not.toBeVisible()
  })

  test('callout facet filters by slow tag', async ({ page }) => {
    const spans = [
      makeSpan({ span_id: 'sp-1', name: 'fast-op', tag: null }),
      makeSpan({ span_id: 'sp-2', name: 'slow-op', tag: 'slow', duration_ns: 300_000_000 }),
    ]
    await stubBackend(page, spans)
    await page.goto('/spans')

    await page.locator('aside button', { hasText: /slow/ }).click()
    await expect(page.getByText('slow-op')).toBeVisible()
    await expect(page.getByText('fast-op')).not.toBeVisible()
  })

  // ── inspector panel ───────────────────────────────────────────────────────

  test('clicking a row opens the inspector panel', async ({ page }) => {
    const span = makeSpan()
    await stubBackend(page, [span])
    await page.goto('/spans')

    await page.getByTestId('span-row-span-1').click()
    await expect(page.getByText('belongs to trace')).toBeVisible()
    await expect(page.getByText('open waterfall')).toBeVisible()
    await expect(page.getByText('attributes')).toBeVisible()
  })

  test('inspector shows duration and service', async ({ page }) => {
    const span = makeSpan({ service_name: 'checkout-svc', duration_ns: 200_000_000 })
    await stubBackend(page, [span])
    await page.goto('/spans')

    await page.getByTestId('span-row-span-1').click()
    // duration appears in inspector header stat (inspector is an aside)
    const inspector = page.getByTestId('span-inspector')
    await expect(inspector.getByText('200.0ms')).toBeVisible()
    await expect(inspector.getByText('checkout-svc')).toBeVisible()
  })

  test('inspector shows attribute key-value pairs', async ({ page }) => {
    const span = makeSpan({ attributes: '{"http.method":"GET","http.status_code":200}' })
    await stubBackend(page, [span])
    await page.goto('/spans')

    await page.getByTestId('span-row-span-1').click()
    await expect(page.getByText('http.method')).toBeVisible()
    await expect(page.getByText('"GET"')).toBeVisible()
  })

  test('open waterfall navigates to trace detail', async ({ page }) => {
    const span = makeSpan({ trace_id: 'trace-xyz' })
    await stubBackend(page, [span])
    await page.route('**/api/traces/trace-xyz', r => jsonResponse(r, []))
    await page.goto('/spans')

    await page.getByTestId('span-row-span-1').click()
    await page.getByText('open waterfall').click()
    await expect(page).toHaveURL('/traces/trace-xyz')
  })

  test('closing inspector with × hides panel', async ({ page }) => {
    const span = makeSpan()
    await stubBackend(page, [span])
    await page.goto('/spans')

    await page.getByTestId('span-row-span-1').click()
    await expect(page.getByText('belongs to trace')).toBeVisible()

    await page.getByTestId('span-inspector').locator('button', { hasText: '×' }).click()
    await expect(page.getByText('belongs to trace')).not.toBeVisible()
  })

  // ── sort control ──────────────────────────────────────────────────────────

  test('sort select is present with time, duration, name options', async ({ page }) => {
    await stubBackend(page, [makeSpan()])
    await page.goto('/spans')

    const sel = page.getByTestId('spans-sort')
    await expect(sel).toBeVisible()
    await expect(sel.getByRole('option', { name: 'time ↓' })).toBeAttached()
    await expect(sel.getByRole('option', { name: 'duration ↓' })).toBeAttached()
    await expect(sel.getByRole('option', { name: 'name a→z' })).toBeAttached()
  })

  // ── status footer ─────────────────────────────────────────────────────────

  test('status footer shows span count and tail hint', async ({ page }) => {
    const spans = [makeSpan(), makeSpan({ span_id: 'sp-2', name: 'other' })]
    await stubBackend(page, spans)
    await page.goto('/spans')

    await expect(page.getByText(/2.*of.*2.*spans/)).toBeVisible()
    await expect(page.getByText('spaniel spans --tail')).toBeVisible()
  })
})
