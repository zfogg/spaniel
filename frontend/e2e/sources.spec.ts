import { test, expect } from '@playwright/test'

const SOURCES = [
  {
    service: 'api-gateway',
    accepted_per_sec: 42.5,
    rejected_per_sec: 0,
    error_rate: 0.02,
    bytes_per_sec: 8192,
    last_seen_ns: Date.now() * 1_000_000,
  },
  {
    service: 'payment-svc',
    accepted_per_sec: 8.1,
    rejected_per_sec: 5.3,
    error_rate: 0.12,
    bytes_per_sec: 1024,
    last_seen_ns: Date.now() * 1_000_000,
  },
  {
    service: 'inventory-svc',
    accepted_per_sec: 3.0,
    rejected_per_sec: 0,
    error_rate: 0,
    bytes_per_sec: 512,
    last_seen_ns: Date.now() * 1_000_000,
  },
]

test.describe('Services page — Sources tab', () => {
  test.beforeEach(async ({ page }) => {
    // Stub the API endpoints needed by the Services page.
    await page.route('**/api/sources', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: SOURCES, meta: { total: 3, page: 1 } }),
      }),
    )
    await page.route('**/api/service-map**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { nodes: [], edges: [] }, meta: { total: 0, page: 1 } }),
      }),
    )
    await page.route('**/api/sessions**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [], meta: { total: 0, page: 1 } }),
      }),
    )
    await page.route('**/api/sessions/active**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: {} }),
      }),
    )
    await page.route('**/api/stats**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, spans_per_sec: 0, logs_per_sec: 0, metrics_per_sec: 0, peak_spans_per_sec: 0 }, meta: { total: 1, page: 1 } }),
      }),
    )
    await page.goto('/services')
  })

  test('Sources tab is visible and switches view', async ({ page }) => {
    const sourcesTab = page.getByTestId('tab-sources')
    await expect(sourcesTab).toBeVisible()

    const mapTab = page.getByTestId('tab-map')
    await expect(mapTab).toBeVisible()

    // Map view is default
    await expect(mapTab).toHaveClass(/bg-surface/)

    // Switch to Sources
    await sourcesTab.click()
    await expect(sourcesTab).toHaveClass(/bg-surface/)
  })

  test('shows all source rows', async ({ page }) => {
    await page.getByTestId('tab-sources').click()

    const rows = page.getByTestId('source-row')
    await expect(rows).toHaveCount(3)

    // Service names visible
    await expect(page.getByText('api-gateway')).toBeVisible()
    await expect(page.getByText('payment-svc')).toBeVisible()
    await expect(page.getByText('inventory-svc')).toBeVisible()
  })

  test('rate-limited badge appears only on rate-limited source', async ({ page }) => {
    await page.getByTestId('tab-sources').click()

    const badges = page.getByTestId('rate-limited-badge')
    // Only payment-svc has rejected_per_sec > 0
    await expect(badges).toHaveCount(1)
  })

  test('rate-limit dot appears on Sources tab button when any source is limited', async ({ page }) => {
    // payment-svc has rejected_per_sec > 0, so the tab button should show a dot
    const dot = page.getByRole('button', { name: /Sources/ }).locator('[aria-label="rate-limited sources"]')
    await expect(dot).toBeVisible()
  })

  test('rows are sorted by accepted_per_sec descending by default', async ({ page }) => {
    await page.getByTestId('tab-sources').click()

    const rows = page.getByTestId('source-row')
    // api-gateway has the highest accepted_per_sec (42.5) so it should be first
    await expect(rows.first()).toContainText('api-gateway')
  })
})
