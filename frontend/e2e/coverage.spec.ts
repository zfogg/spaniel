import { test, expect, type Route, type Page } from '@playwright/test'
import type { CoverageReport } from '../src/lib/api'

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

async function stubChrome(page: Page) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/stats*', r => jsonResponse(r, {
    span_count: 0, trace_count: 0, log_count: 0, db_size: 0,
    session_count: 0, oldest_session_at: 0,
  }))
  await page.route('**/api/forwarders', r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
}

async function stubCoverage(page: Page, report: CoverageReport | null) {
  await page.route(url => new URL(url.toString()).pathname === '/api/coverage',
    r => jsonResponse(r, report ?? { services: [], overall: { observed_operations: 0, total_routes: 0, dark_count: 0, coverage_pct: 0 } }))
}

const fullReport: CoverageReport = {
  services: [
    {
      name: 'api', source: 'openapi', spec: 'openapi.yaml',
      observed_operations: 2, total_routes: 4, coverage_pct: 50,
      observed_routes: [
        { method: 'GET',  path: '/api/products', hits: 241, p95_ns: 78_000_000 },
        { method: 'POST', path: '/api/cart',     hits: 46,  p95_ns: 46_000_000 },
      ],
      dark_routes: [
        { method: 'POST',   path: '/api/v1/export',     hits: 0 },
        { method: 'DELETE', path: '/api/v1/users/:id',  hits: 0 },
      ],
    },
    {
      name: 'pricing', source: 'observed',
      observed_operations: 1, total_routes: 1, coverage_pct: 100,
      observed_routes: [
        { method: 'RPC', path: 'pricing.v1.Quote', hits: 34, p95_ns: 80_000_000 },
      ],
      dark_routes: [],
    },
  ],
  overall: { observed_operations: 3, total_routes: 5, dark_count: 2, coverage_pct: 60 },
}

test.describe('Coverage page', () => {
  test('renders the empty state when no services exist', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, null)
    await page.goto('/coverage')
    await expect(page.getByText('No coverage data')).toBeVisible()
    await expect(page.getByText(/--routes-file/)).toBeVisible()
  })

  test('renders the overall card with rolled-up stats', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    // Page heading + intro.
    await expect(page.getByRole('heading', { name: 'Coverage' })).toBeVisible()
    await expect(page.getByText(/cross-references every/)).toBeVisible()

    // Overall percentage.
    await expect(page.getByText('60.0')).toBeVisible()
    await expect(page.getByText('3 of 5 routes')).toBeVisible()
  })

  test('lists each service with its source label and coverage %', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    // Service rows — the "best" stat box also renders the service name, so
    // these locators just confirm presence rather than uniqueness.
    await expect(page.getByText('api', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('pricing', { exact: true }).first()).toBeVisible()

    // Source labels.
    await expect(page.getByText('★ openapi.yaml')).toBeVisible()
    await expect(page.getByText('observed only · no spec')).toBeVisible()

    // Per-service coverage %.
    await expect(page.getByText('50.0').first()).toBeVisible()
    await expect(page.getByText('100.0').first()).toBeVisible()
  })

  test('shows the route heatmap with hit counts and DARK markers', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    // Observed route cells (hits + path visible).
    await expect(page.getByText('/api/products')).toBeVisible()
    await expect(page.getByText('241')).toBeVisible()

    // Dark route cells.
    await expect(page.getByText('/api/v1/export')).toBeVisible()
    await expect(page.getByText('/api/v1/users/:id')).toBeVisible()
    await expect(page.getByText('DARK').first()).toBeVisible()
  })

  test('dark filter narrows the heatmap to dark routes only', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    await page.getByRole('button', { name: /dark only/i }).click()

    // Dark routes still visible.
    await expect(page.getByText('/api/v1/export')).toBeVisible()
    // Observed routes gone from the heatmap (they don't appear in the inspector either, since nothing selected).
    await expect(page.getByText('/api/products')).toHaveCount(0)
  })

  test('clicking a dark route opens the inspector with the "instrument this route" hint', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    await page.getByText('/api/v1/export').click()

    await expect(page.getByText('no traces — instrument this route')).toBeVisible()
    // Inspector echoes the http.route hint.
    await expect(page.getByText(/http\.route = "\/api\/v1\/export"/)).toBeVisible()
  })

  test('clicking an observed route shows hits + p95 stats in the inspector', async ({ page }) => {
    await stubChrome(page)
    await stubCoverage(page, fullReport)
    await page.goto('/coverage')

    await page.getByText('/api/products').click()

    await expect(page.getByText('traces seen')).toBeVisible()
    await expect(page.getByText('p95', { exact: true })).toBeVisible()
    // p95 = 78ms (formatted).
    await expect(page.getByText('78.0ms')).toBeVisible()
  })
})
