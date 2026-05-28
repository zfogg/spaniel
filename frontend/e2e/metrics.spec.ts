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
    traces?: Array<{ trace_id: string; op: string; service: string; status_code: number; start_ns: number; end_ns: number; duration_ns: number }>
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

  // Predicate matcher — globs interpret `?` as a wildcard, so we use a
  // function to disambiguate /api/metrics from /api/metrics/series cleanly.
  await page.route(
    url => {
      const path = new URL(url.toString()).pathname
      return path === '/api/metrics' || path === '/api/metrics/series'
    },
    r => {
      const url = new URL(r.request().url())
      if (url.pathname === '/api/metrics/series') {
        const name = url.searchParams.get('name') || ''
        const service = url.searchParams.get('service') || ''
        const series = fx.series[`${service}/${name}`]
        // Always include the `traces` field (the server contract guarantees [] when none).
        const filled = series
          ? { ...series, traces: series.traces ?? [] }
          : { name, service_name: service, type: 'gauge', unit: '', description: '', points: [], traces: [] }
        return jsonResponse(r, filled)
      }
      return jsonResponse(r, fx.catalog)
    },
  )
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

    // metric names in the sidebar — use .first() since the page may auto-select
    // the first metric, causing its name to appear in both the list and heading
    await expect(page.getByText('http.requests', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('http.dur', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('pool.in_use', { exact: true }).first()).toBeVisible()

    // service group headers — exact:true would fail because each header's
    // textContent includes a trailing count badge ("postgres1"), so we
    // match by substring and just confirm presence.
    await expect(page.getByText(/^postgres/).first()).toBeVisible()

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

    // Sidebar metric rows are <button>s — scoping by role keeps us out of
    // the right-pane H1 which still shows the auto-selected metric.
    await expect(page.getByRole('button', { name: /http\.requests/i })).toBeVisible()
    await page.getByPlaceholder(/search metrics/i).fill('pool')

    await expect(page.getByRole('button', { name: /http\.requests/i })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /pool\.in_use/i })).toBeVisible()
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
    await expect(page.getByRole('button', { name: /http\.requests/i })).toBeVisible()

    // Both the kind tag and the filter chip are clickable; the filter chip
    // is the one inside the sidebar header (last in DOM order).
    const gaugeChips = page.getByRole('button', { name: /^gauge$/i })
    await gaugeChips.last().click()

    await expect(page.getByRole('button', { name: /http\.requests/i })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /pool\.in_use/i })).toBeVisible()
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

test.describe('Metrics — trace overlay + correlated panel', () => {
  test('renders dotted markers + correlated-traces table, "open" navigates to /traces/:id', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: 'inbound', sample_count: 4 },
      ],
      series: {
        'api/http.requests': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: 'inbound',
          points: [
            { timestamp_ns: 1_000_000_000, value: 1 },
            { timestamp_ns: 2_000_000_000, value: 4 },
            { timestamp_ns: 3_000_000_000, value: 7 },
            { timestamp_ns: 4_000_000_000, value: 9 },
          ],
          traces: [
            { trace_id: 'abc1230000000000', op: 'GET /cart',     service: 'api', status_code: 1, start_ns: 1_500_000_000, end_ns: 1_600_000_000, duration_ns: 100_000_000 },
            { trace_id: 'def4560000000000', op: 'POST /checkout', service: 'api', status_code: 2, start_ns: 3_500_000_000, end_ns: 3_600_000_000, duration_ns: 100_000_000 },
          ],
        },
      },
    })
    await page.goto('/metrics')
    // Metrics auto-selects the first metric — wait for the chart H1 to confirm.
    await expect(page.locator('h1', { hasText: 'http.requests' })).toBeVisible()

    // Vertical dotted markers: two of them, one per trace.
    await expect(page.getByTestId('metric-trace-marker')).toHaveCount(2)

    // Correlated-traces table: rows + "open →" buttons.
    const panel = page.getByTestId('correlated-traces')
    await expect(panel).toBeVisible()
    await expect(panel.getByText('GET /cart')).toBeVisible()
    await expect(panel.getByText('POST /checkout')).toBeVisible()
    await expect(panel.getByText('abc12300…')).toBeVisible()

    // Click "open →" on the first trace row → navigate to /traces/:id.
    await panel.getByRole('button', { name: /open/ }).first().click()
    await expect(page).toHaveURL(/\/traces\/abc1230000000000$/)
  })

  test('hides the correlated panel when no traces fell in the window', async ({ page }) => {
    await stubMetrics(page, {
      catalog: [
        { name: 'pool.in_use', service_name: 'postgres', type: 'gauge', unit: 'conn', description: '', sample_count: 2 },
      ],
      series: {
        'postgres/pool.in_use': {
          name: 'pool.in_use', service_name: 'postgres', type: 'gauge', unit: 'conn', description: '',
          points: [
            { timestamp_ns: 1, value: 3 },
            { timestamp_ns: 2, value: 5 },
          ],
          traces: [],
        },
      },
    })
    await page.goto('/metrics')
    await expect(page.locator('h1', { hasText: 'pool.in_use' })).toBeVisible()
    await expect(page.getByTestId('correlated-traces')).toHaveCount(0)
    await expect(page.getByTestId('metric-trace-marker')).toHaveCount(0)
  })
})
