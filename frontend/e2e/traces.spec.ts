import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

interface Fixture {
  traces?: unknown[]
  services?: string[]
  stats?: Record<string, number>
  forwarders?: unknown[]
}

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

async function stubBackend(page: Page, fx: Fixture) {
  // Block any websocket attempt cleanly — we don't drive realtime spans here.
  await page.routeWebSocket('**/ws', ws => ws.close())

  await page.route('**/api/traces*', r => jsonResponse(r, fx.traces ?? []))
  await page.route('**/api/services', r => jsonResponse(r, fx.services ?? []))
  await page.route('**/api/stats*', r => jsonResponse(r, fx.stats ?? { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 }))
  await page.route('**/api/forwarders', r => jsonResponse(r, fx.forwarders ?? []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
  await page.route('**/api/sessions', r => jsonResponse(r, []))
  await page.route('**/api/lint*', r => jsonResponse(r, []))
}

function trace(id: string, service: string, name: string, durationNs: number, statusCode = 1, hasN1 = false) {
  return {
    trace_id: id,
    service_name: service,
    name,
    status_code: statusCode,
    start_ns: Date.now() * 1_000_000,
    end_ns: Date.now() * 1_000_000 + durationNs,
    duration_ns: durationNs,
    session_id: 's1',
    has_n1: hasN1,
  }
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Traces page', () => {
  test('renders the empty state when no traces exist', async ({ page }) => {
    await stubBackend(page, { traces: [], services: [] })
    await page.goto('/')

    await expect(page.getByText('Waiting for traces…')).toBeVisible()
    // OTLP endpoint pills — text appears twice (pill + env-var snippet); strict mode requires uniqueness.
    await expect(page.getByText(':4318', { exact: true })).toBeVisible()
    await expect(page.getByText(':4317', { exact: true })).toBeVisible()
    // Step-1 env vars are shown
    await expect(page.getByText(/OTEL_EXPORTER_OTLP_ENDPOINT/)).toBeVisible()
  })

  test('lets the empty state switch between language SDK tabs', async ({ page }) => {
    await stubBackend(page, { traces: [], services: [] })
    await page.goto('/')

    // Python default — pip command is visible.
    await expect(page.getByText(/opentelemetry-distro opentelemetry-exporter-otlp/)).toBeVisible()

    await page.getByRole('button', { name: 'Node.js' }).click()
    await expect(page.getByText(/@opentelemetry\/auto-instrumentations-node/)).toBeVisible()

    await page.getByRole('button', { name: 'Go' }).click()
    await expect(page.getByText(/go\.opentelemetry\.io\/otel/)).toBeVisible()

    await page.getByRole('button', { name: 'Java' }).click()
    await expect(page.getByText(/opentelemetry-javaagent/)).toBeVisible()
  })

  test('renders a populated traces table', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace('aaa111', 'api',     'GET /cart',     12_000_000,  1, false),
        trace('bbb222', 'pricing', 'pricing.Quote',  80_000_000,  1, true),
        trace('ccc333', 'api',     'POST /checkout', 320_000_000, 2, false),
      ],
      services: ['api', 'pricing'],
    })
    await page.goto('/')

    // Table header row.
    await expect(page.getByRole('columnheader', { name: 'Service' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Root Span' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Duration' })).toBeVisible()

    // Three data rows.
    await expect(page.getByText('GET /cart')).toBeVisible()
    await expect(page.getByText('pricing.Quote')).toBeVisible()
    await expect(page.getByText('POST /checkout')).toBeVisible()

    // Status badges and N+1 badge.
    await expect(page.getByText('ERROR')).toBeVisible()
    await expect(page.getByText('N+1')).toBeVisible()

    // Duration formatting.
    await expect(page.getByText('12.0ms')).toBeVisible()
    await expect(page.getByText('320.0ms')).toBeVisible()
  })

  test('filters traces by service via the select', async ({ page }) => {
    await stubBackend(page, {
      traces: [
        trace('aaa111', 'api',     'GET /cart',     12_000_000),
        trace('bbb222', 'pricing', 'pricing.Quote', 80_000_000),
      ],
      services: ['api', 'pricing'],
    })
    await page.goto('/')

    await expect(page.getByText('GET /cart')).toBeVisible()
    await expect(page.getByText('pricing.Quote')).toBeVisible()

    // Open the service filter and pick "pricing".
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'pricing' }).click()

    await expect(page.getByText('pricing.Quote')).toBeVisible()
    await expect(page.getByText('GET /cart')).toHaveCount(0)
  })

  test('clicking a row navigates to the trace detail URL', async ({ page }) => {
    // Stub the detail endpoint so the navigation lands on a page that doesn't error.
    await stubBackend(page, {
      traces: [trace('aaa111', 'api', 'GET /cart', 12_000_000)],
      services: ['api'],
    })
    await page.route('**/api/traces/aaa111', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))

    await page.goto('/')
    await page.getByText('GET /cart').click()

    await expect(page).toHaveURL(/\/traces\/aaa111$/)
  })
})
