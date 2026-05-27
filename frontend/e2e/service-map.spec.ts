import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface ServiceMapFixture {
  nodes: Array<{
    id: string
    span_count: number
    error_count: number
    p95_ns?: number
    top_operations?: Array<{ name: string; count: number; p95_ns: number }>
  }>
  edges: Array<{
    from: string
    to: string
    call_count: number
    avg_duration_ns: number
    error_count?: number
  }>
}

async function stubServiceMap(page: Page, fx: ServiceMapFixture) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/forwarders',      r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
  await page.route('**/api/sessions',        r => jsonResponse(r, [
    { id: 'sess-1', label: 'session-1', created_at: 1, is_baseline: false, is_imported: false, span_count: 0, trace_count: 0, services: '[]' },
    { id: 'sess-2', label: 'session-2', created_at: 2, is_baseline: false, is_imported: false, span_count: 0, trace_count: 0, services: '[]' },
  ]))
  await page.route('**/api/service-map*',    r => jsonResponse(r, fx))
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('ServiceMap page', () => {
  test('shows empty state when there are no nodes', async ({ page }) => {
    await stubServiceMap(page, { nodes: [], edges: [] })
    await page.goto('/services')

    await expect(page.getByText('No spans yet')).toBeVisible()
  })

  test('renders a single node with its service name', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [{ id: 'api-gateway', span_count: 10, error_count: 0 }],
      edges: [],
    })
    await page.goto('/services')

    // The node id is rendered as SVG <text> inside SvgNode.
    // SVG <text> elements aren't matched by Playwright's locator('text') shorthand —
    // use a CSS selector with the svg| namespace prefix instead.
    await expect(page.locator('svg text').filter({ hasText: /^api-gateway$/ }).first()).toBeVisible()
  })

  test('renders two nodes with an edge between them', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [
        { id: 'frontend', span_count: 5,  error_count: 0 },
        { id: 'backend',  span_count: 15, error_count: 0 },
      ],
      edges: [{ from: 'frontend', to: 'backend', call_count: 3, avg_duration_ns: 50_000_000 }],
    })
    await page.goto('/services')

    await expect(page.locator('svg text').filter({ hasText: /^frontend$/ }).first()).toBeVisible()
    await expect(page.locator('svg text').filter({ hasText: /^backend$/ }).first()).toBeVisible()
    // Footer shows services · edges summary
    await expect(page.getByText(/2 services/)).toBeVisible()
    await expect(page.getByText(/1 edges/)).toBeVisible()
  })

  test('clicking a node shows the "View traces for…" button in the header', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [{ id: 'my-service', span_count: 8, error_count: 0 }],
      edges: [],
    })
    await page.goto('/services')

    // Click on the SVG <text> element that renders the service name
    await page.locator('svg text').filter({ hasText: /^my-service$/ }).first().click()

    // After clicking, the header "View traces for X →" button appears
    await expect(page.getByText(/View traces for my-service/)).toBeVisible()
  })

  test('clicking a node opens the inspector panel with stats + top operations', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [{
        id: 'pricing',
        span_count: 24,
        error_count: 3,
        p95_ns: 80_000_000,
        top_operations: [
          { name: 'pricing.Quote',  count: 18, p95_ns: 78_000_000 },
          { name: 'pricing.Bulk',   count: 6,  p95_ns: 140_000_000 },
        ],
      }],
      edges: [],
    })
    await page.goto('/services')

    await page.getByTestId('node-pricing').click()

    const inspector = page.getByTestId('node-inspector')
    await expect(inspector).toBeVisible()
    await expect(inspector.getByText('spans')).toBeVisible()
    await expect(inspector.getByText('24')).toBeVisible()           // span count
    await expect(inspector.getByText('80.0ms')).toBeVisible()       // p95
    await expect(inspector.getByText('3', { exact: true })).toBeVisible()  // errors stat
    await expect(inspector.getByText('12.5%')).toBeVisible()        // err rate 3/24
    await expect(inspector.getByText('pricing.Quote')).toBeVisible()
    await expect(inspector.getByText(/18× · 78\.0ms p95/)).toBeVisible()
  })

  test('inspector closes via the × button', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [{ id: 'svc', span_count: 1, error_count: 0, p95_ns: 1000, top_operations: [] }],
      edges: [],
    })
    await page.goto('/services')

    await page.getByTestId('node-svc').click()
    await expect(page.getByTestId('node-inspector')).toBeVisible()
    await page.getByRole('button', { name: 'close inspector' }).click()
    await expect(page.getByTestId('node-inspector')).toHaveCount(0)
  })

  test('edge labels render call_count + avg duration', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [
        { id: 'api', span_count: 5, error_count: 0, p95_ns: 0, top_operations: [] },
        { id: 'db',  span_count: 5, error_count: 0, p95_ns: 0, top_operations: [] },
      ],
      edges: [{ from: 'api', to: 'db', call_count: 142, avg_duration_ns: 38_000_000, error_count: 0 }],
    })
    await page.goto('/services')
    // Edge label format from the issue: "142 calls · 38ms avg".
    await expect(page.locator('svg text').filter({ hasText: /142 calls · 38\.0ms avg/ })).toBeVisible()
  })

  test('edge carrying errors gets a danger stroke + " · X err" label suffix', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [
        { id: 'api', span_count: 5, error_count: 0, p95_ns: 0, top_operations: [] },
        { id: 'db',  span_count: 5, error_count: 2, p95_ns: 0, top_operations: [] },
      ],
      edges: [{ from: 'api', to: 'db', call_count: 4, avg_duration_ns: 20_000_000, error_count: 2 }],
    })
    await page.goto('/services')
    await expect(page.locator('svg text').filter({ hasText: /· 2 err/ })).toBeVisible()
  })

  test('node with error_count > 0 gets a danger border (data-error="true")', async ({ page }) => {
    await stubServiceMap(page, {
      nodes: [
        { id: 'happy', span_count: 5, error_count: 0, p95_ns: 0, top_operations: [] },
        { id: 'sad',   span_count: 5, error_count: 1, p95_ns: 0, top_operations: [] },
      ],
      edges: [],
    })
    await page.goto('/services')
    await expect(page.getByTestId('node-happy')).toHaveAttribute('data-error', 'false')
    await expect(page.getByTestId('node-sad')).toHaveAttribute('data-error', 'true')
  })

  test('session filter dropdown lists available sessions and refetches with sessionId', async ({ page }) => {
    let lastUrl = ''
    await page.routeWebSocket('**/ws', ws => ws.close())
    await page.route('**/api/forwarders',      r => jsonResponse(r, []))
    await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
    await page.route('**/api/sessions',        r => jsonResponse(r, [
      { id: 'sess-1', label: 'session-1', created_at: 1, is_baseline: false, is_imported: false, span_count: 0, trace_count: 0, services: '[]' },
      { id: 'sess-2', label: 'session-2', created_at: 2, is_baseline: false, is_imported: false, span_count: 0, trace_count: 0, services: '[]' },
    ]))
    await page.route('**/api/service-map*', r => {
      lastUrl = r.request().url()
      return jsonResponse(r, { nodes: [], edges: [] })
    })

    await page.goto('/services')
    await page.getByTestId('session-filter').selectOption('sess-2')

    await expect.poll(() => lastUrl).toContain('sessionId=sess-2')
  })
})
