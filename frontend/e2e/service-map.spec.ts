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
  nodes: Array<{ id: string; span_count: number; error_count: number }>
  edges: Array<{ from: string; to: string; call_count: number; avg_duration_ns: number }>
}

async function stubServiceMap(page: Page, fx: ServiceMapFixture) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/forwarders',      r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
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
})
