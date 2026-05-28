import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface Fixture {
  traces?: unknown[]
  services?: string[]
}

async function stubBackend(page: Page, fx: Fixture) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const pathname = new URL(r.request().url()).pathname
    if (pathname === '/api/stats')
      return jsonResponse(r, { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 })
    if (pathname === '/api/forwarders') return jsonResponse(r, [])
    if (pathname === '/api/sessions/active') return jsonResponse(r, { id: '', label: '' })
    if (pathname === '/api/sessions') return jsonResponse(r, [])
    if (pathname === '/api/services') return jsonResponse(r, fx.services ?? [])
    if (pathname.startsWith('/api/lint')) return jsonResponse(r, [])
    if (pathname.startsWith('/api/traces')) return jsonResponse(r, fx.traces ?? [])
    return r.continue()
  })
}

function trace(overrides: {
  id: string
  service?: string
  name?: string
  durationNs?: number
  statusCode?: number
  hasN1?: boolean
  spanCount?: number
}) {
  return {
    trace_id: overrides.id,
    service_name: overrides.service ?? 'api',
    name: overrides.name ?? 'GET /',
    status_code: overrides.statusCode ?? 1,
    start_ns: Date.now() * 1_000_000,
    end_ns: Date.now() * 1_000_000 + (overrides.durationNs ?? 10_000_000),
    duration_ns: overrides.durationNs ?? 10_000_000,
    session_id: 's1',
    session_label: 'live',
    has_n1: overrides.hasN1 ?? false,
    span_count: overrides.spanCount ?? 3,
  }
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Traces page', () => {
  test('renders the empty state when no traces exist', async ({ page }) => {
    await stubBackend(page, { traces: [], services: [] })
    await page.goto('/')

    await expect(page.getByText('Waiting for traces…')).toBeVisible()
    await expect(page.getByText(':4318', { exact: true })).toBeVisible()
    await expect(page.getByText(':4317', { exact: true })).toBeVisible()
    await expect(page.getByText(/OTEL_EXPORTER_OTLP_ENDPOINT/)).toBeVisible()
  })

  test('lets the empty state switch between language SDK tabs', async ({ page }) => {
    await stubBackend(page, { traces: [], services: [] })
    await page.goto('/')

    await expect(page.getByText(/opentelemetry-distro opentelemetry-exporter-otlp/)).toBeVisible()

    await page.getByRole('button', { name: 'Node.js' }).click()
    await expect(page.getByText(/@opentelemetry\/auto-instrumentations-node/)).toBeVisible()

    await page.getByRole('button', { name: 'Go' }).click()
    await expect(page.getByText(/go\.opentelemetry\.io\/otel/)).toBeVisible()

    await page.getByRole('button', { name: 'Java' }).click()
    await expect(page.getByText(/opentelemetry-javaagent/)).toBeVisible()
  })

  test('renders the column header row', async ({ page }) => {
    await stubBackend(page, {
      traces: [trace({ id: 'aaa111', name: 'GET /cart', durationNs: 12_000_000 })],
    })
    await page.goto('/')

    const main = page.getByRole('main')
    await expect(main.getByText('operation', { exact: true })).toBeVisible()
    await expect(main.getByText('dur',       { exact: true })).toBeVisible()
    await expect(main.getByText('spans',     { exact: true })).toBeVisible()
    await expect(main.getByText('shape',     { exact: true })).toBeVisible()
    await expect(main.getByText('ago',       { exact: true })).toBeVisible()
  })

  test('renders operation name, duration, and span count', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'aaa111', name: 'GET /cart',     durationNs: 12_000_000,  spanCount: 5 }),
        trace({ id: 'bbb222', name: 'pricing.Quote', durationNs: 80_000_000,  spanCount: 12 }),
        trace({ id: 'ccc333', name: 'POST /checkout', durationNs: 320_000_000, spanCount: 8 }),
      ],
      services: ['api', 'pricing'],
    })
    await page.goto('/')

    await expect(page.getByText('GET /cart')).toBeVisible()
    await expect(page.getByText('pricing.Quote')).toBeVisible()
    await expect(page.getByText('POST /checkout')).toBeVisible()

    // Duration formatting.
    await expect(page.getByText('12.0ms')).toBeVisible()
    await expect(page.getByText('320.0ms')).toBeVisible()

    // Span count column.
    await expect(page.getByTestId('trace-row-aaa111').getByText('5', { exact: true })).toBeVisible()
    await expect(page.getByTestId('trace-row-bbb222').getByText('12', { exact: true })).toBeVisible()
  })

  test('n+1 chip appears for traces with has_n1=true', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'n1-trace', name: 'pricing.Quote', durationNs: 80_000_000, hasN1: true }),
      ],
    })
    await page.goto('/')

    await expect(page.getByTestId('trace-row-n1-trace').getByText('n+1', { exact: true })).toBeVisible()
  })

  test('slow chip appears for traces with duration > 250ms', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'slow-trace', name: 'POST /checkout', durationNs: 300_000_000 }),
      ],
    })
    await page.goto('/')

    await expect(page.getByTestId('trace-row-slow-trace').getByText('slow', { exact: true })).toBeVisible()
  })

  test('error chip appears for traces with status_code=2', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'err-trace', name: 'GET /fail', durationNs: 10_000_000, statusCode: 2 }),
      ],
    })
    await page.goto('/')

    await expect(page.getByTestId('trace-row-err-trace').getByText('error', { exact: true })).toBeVisible()
  })

  test('shape bar fill width is proportional to duration', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'fast', name: 'fast-op', durationNs: 50_000_000 }),
        trace({ id: 'slow', name: 'slow-op', durationNs: 200_000_000 }),
      ],
    })
    await page.goto('/')

    // The fastest trace (50ms, max=200ms) should have a bar at 25% width.
    // The slowest (200ms, max=200ms) should be at 100%.
    const fastBar = page.getByTestId('trace-row-fast').locator('.rounded-full.opacity-80')
    const slowBar = page.getByTestId('trace-row-slow').locator('.rounded-full.opacity-80')

    const fastWidth = await fastBar.evaluate((el: HTMLElement) => el.style.width)
    const slowWidth = await slowBar.evaluate((el: HTMLElement) => el.style.width)

    expect(fastWidth).toBe('25%')
    expect(slowWidth).toBe('100%')
  })

  test('filters traces by service via the select', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace({ id: 'aaa111', service: 'api',     name: 'GET /cart',     durationNs: 12_000_000 }),
        trace({ id: 'bbb222', service: 'pricing', name: 'pricing.Quote', durationNs: 80_000_000 }),
      ],
      services: ['api', 'pricing'],
    })
    await page.goto('/')

    await expect(page.getByText('GET /cart')).toBeVisible()
    await expect(page.getByText('pricing.Quote')).toBeVisible()

    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'pricing' }).click()

    await expect(page.getByText('pricing.Quote')).toBeVisible()
    await expect(page.getByText('GET /cart')).toHaveCount(0)
  })

  test('clicking a row navigates to the trace detail URL', async ({ page }) => {
    await stubBackend(page, {
      traces: [trace({ id: 'aaa111', name: 'GET /cart', durationNs: 12_000_000 })],
      services: ['api'],
    })

    await page.goto('/')
    await page.getByTestId('trace-row-aaa111').click()

    await expect(page).toHaveURL(/\/traces\/aaa111$/)
  })
})
