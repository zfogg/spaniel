import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface MetricFixture {
  catalog: Array<{
    name: string
    service_name: string
    type: 'gauge' | 'counter' | 'histogram'
    unit: string
    description: string
    sample_count: number
  }>
  // keyed by "<service>/<name>"
  series: Record<string, {
    name: string
    service_name: string
    type: 'gauge' | 'counter' | 'histogram'
    unit: string
    description: string
    points: Array<{ timestamp_ns: number; value: number; percentile?: 'p50' | 'p95' | 'p99' }>
  }>
}

async function stubMetrics(page: Page, fx: MetricFixture) {
  // Block websocket and the chrome periphery so the Metrics page is the only thing exercised.
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/stats*', r => jsonResponse(r, {
    span_count: 0, trace_count: 0, log_count: 0, db_size: 0,
    session_count: 0, oldest_session_at: 0,
  }))
  await page.route('**/api/forwarders', r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))

  await page.route('**/api/metrics', r => jsonResponse(r, fx.catalog))
  await page.route('**/api/metrics?**', r => jsonResponse(r, fx.catalog))

  await page.route('**/api/metrics/series?**', r => {
    const url = new URL(r.request().url())
    const name = url.searchParams.get('name') || ''
    const service = url.searchParams.get('service') || ''
    const key = `${service}/${name}`
    const series = fx.series[key]
    if (!series) {
      return jsonResponse(r, { name, service_name: service, type: 'gauge', unit: '', description: '', points: [] })
    }
    return jsonResponse(r, series)
  })
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Metrics page', () => {
  test('renders the empty state when no metrics exist', async ({ page }) => {
    await stubMetrics(page, { catalog: [], series: {} })
    await page.goto('/metrics')

    await expect(page.getByText('No metrics yet')).toBeVisible()
    await expect(page.getByText(/localhost:4318\/v1\/metrics/)).toBeVisible()
  })

  test('renders the catalog grouped by service with kind tags', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter',   unit: 'req',  description: 'inbound HTTP requests', sample_count: 12 },
        { name: 'http.dur',      service_name: 'api',      type: 'histogram', unit: 'ms',   description: 'request duration',      sample_count: 30 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',     unit: 'conn', description: 'live connections',      sample_count: 8  },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req',
          description: 'inbound HTTP requests',
          points: [
            { timestamp_ns: 1, value: 5 },
            { timestamp_ns: 2, value: 8 },
          ],
        },
      },
    })

    await page.goto('/metrics')

    // metric names in the sidebar
    await expect(page.getByText('http.requests', { exact: true })).toBeVisible()
    await expect(page.getByText('http.dur', { exact: true })).toBeVisible()
    await expect(page.getByText('pool.in_use', { exact: true })).toBeVisible()

    // service group headers
    await expect(page.getByText('api', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('postgres', { exact: true }).first()).toBeVisible()

    // kind tags exist for each type
    await expect(page.getByText('counter').first()).toBeVisible()
    await expect(page.getByText('histogram').first()).toBeVisible()
    await expect(page.getByText('gauge').first()).toBeVisible()

    // total metric count chip
    await expect(page.getByText('3 metrics')).toBeVisible()
  })

  test('filters the catalog by search query', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter', unit: 'req',  description: '', sample_count: 1 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',   unit: 'conn', description: '', sample_count: 1 },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: '',
          points: [{ timestamp_ns: 1, value: 1 }],
        },
      },
    })
    await page.goto('/metrics')

    await expect(page.getByText('http.requests', { exact: true })).toBeVisible()
    await page.getByPlaceholder(/search metrics/i).fill('pool')

    await expect(page.getByText('http.requests')).toHaveCount(0)
    await expect(page.getByText('pool.in_use', { exact: true })).toBeVisible()
    await expect(page.getByText('1 metrics')).toBeVisible()
  })

  test('filters the catalog by the gauge type chip', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter', unit: 'req',  description: '', sample_count: 1 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',   unit: 'conn', description: '', sample_count: 1 },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: '',
          points: [{ timestamp_ns: 1, value: 1 }],
        },
      },
    })
    await page.goto('/metrics')
    await expect(page.getByText('http.requests', { exact: true })).toBeVisible()

    // The first GAUGE-labelled element is the kind tag on the row; the second is
    // the filter chip in the sidebar header. Click the filter chip.
    const gaugeChips = page.getByRole('button', { name: /^gauge$/i })
    await gaugeChips.last().click()

    await expect(page.getByText('http.requests')).toHaveCount(0)
    await expect(page.getByText('pool.in_use', { exact: true })).toBeVisible()
  })

  test('auto-selects the first metric and renders its chart + counter stats', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: 'request count', sample_count: 2 },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req',
          description: 'request count',
          points: [
            { timestamp_ns: 1_000_000_000, value: 5 },
            { timestamp_ns: 2_000_000_000, value: 8 },
            { timestamp_ns: 3_000_000_000, value: 12 },
          ],
        },
      },
    })
    await page.goto('/metrics')

    // The right pane H1 echoes the selected metric name.
    await expect(page.locator('h1', { hasText: 'http.requests' })).toBeVisible()
    await expect(page.getByText('request count')).toBeVisible()
    await expect(page.getByText('unit · req')).toBeVisible()

    // Counter stat strip labels.
    await expect(page.getByText('rate / min')).toBeVisible()
    await expect(page.getByText('total', { exact: true })).toBeVisible()
    await expect(page.getByText('peak', { exact: true })).toBeVisible()

    // Chart SVG is present.
    await expect(page.locator('svg').first()).toBeVisible()
  })

  test('histogram view shows p50 / p95 / p99 legend and stats', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.dur', service_name: 'api', type: 'histogram', unit: 'ms', description: 'request duration', sample_count: 9 },
      ],
      series: {
        'api/http.dur': {
          name: 'http.dur', service_name: 'api', type: 'histogram', unit: 'ms',
          description: 'request duration',
          points: [
            { timestamp_ns: 1_000_000_000, value: 10,  percentile: 'p50' },
            { timestamp_ns: 1_000_000_000, value: 80,  percentile: 'p95' },
            { timestamp_ns: 1_000_000_000, value: 140, percentile: 'p99' },
            { timestamp_ns: 2_000_000_000, value: 12,  percentile: 'p50' },
            { timestamp_ns: 2_000_000_000, value: 90,  percentile: 'p95' },
            { timestamp_ns: 2_000_000_000, value: 160, percentile: 'p99' },
          ],
        },
      },
    })
    await page.goto('/metrics')

    await expect(page.locator('h1', { hasText: 'http.dur' })).toBeVisible()

    // Stat strip uses p50 / p95 / p99 / max p99 for histograms.
    await expect(page.getByText('p50', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('p95', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('p99', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('max p99', { exact: true })).toBeVisible()
  })

  test('selecting a different metric in the sidebar swaps the chart', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter', unit: 'req',  description: 'A',  sample_count: 1 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',   unit: 'conn', description: 'B',  sample_count: 1 },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: 'A',
          points: [{ timestamp_ns: 1, value: 1 }],
        },
        'postgres/pool.in_use': {
          name: 'pool.in_use', service_name: 'postgres', type: 'gauge', unit: 'conn', description: 'B',
          points: [{ timestamp_ns: 1, value: 4 }, { timestamp_ns: 2, value: 7 }],
        },
      },
    })
    await page.goto('/metrics')

    // First metric auto-selected.
    await expect(page.locator('h1', { hasText: 'http.requests' })).toBeVisible()
    await expect(page.getByText('rate / min')).toBeVisible()

    // Switch to the gauge — the H1 + stat-strip labels should change.
    await page.getByRole('button', { name: /pool\.in_use/ }).click()
    await expect(page.locator('h1', { hasText: 'pool.in_use' })).toBeVisible()
    await expect(page.getByText('now', { exact: true })).toBeVisible()
    await expect(page.getByText('5m ago')).toBeVisible()
  })
})
