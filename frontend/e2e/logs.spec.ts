import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface Log {
  timestamp_ns: number
  trace_id: string
  span_id: string
  severity: number
  body: string
  attributes: string
  service_name: string
  session_id: string
  received_at: number
}

function makeLog(overrides: Partial<Log> = {}): Log {
  return {
    timestamp_ns: Date.now() * 1_000_000,
    trace_id:     '0000000000000000',
    span_id:      'span-001',
    severity:     9,   // INFO
    body:         'default log message',
    attributes:   '{}',
    service_name: 'my-service',
    session_id:   's1',
    received_at:  0,
    ...overrides,
  }
}

async function stubLogs(page: Page, logs: Log[], services: string[] = []) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/forwarders',      r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
  await page.route('**/api/services',        r => jsonResponse(r, services))
  await page.route('**/api/logs*',           r => jsonResponse(r, logs))
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('LogViewer', () => {
  test('shows empty state when there are no logs', async ({ page }) => {
    await stubLogs(page, [])
    await page.goto('/logs')

    await expect(page.getByText('no logs yet — send traces with OTel logging attached')).toBeVisible()
  })

  test('a log row shows severity badge, service name, and body text', async ({ page }) => {
    await stubLogs(page, [
      makeLog({ severity: 9, service_name: 'auth-service', body: 'User logged in successfully' }),
    ])
    await page.goto('/logs')

    await expect(page.getByText('INFO').first()).toBeVisible()
    await expect(page.getByText('auth-service')).toBeVisible()
    await expect(page.getByText('User logged in successfully')).toBeVisible()
  })

  test('clicking the ERROR chip hides non-error logs', async ({ page }) => {
    await stubLogs(page, [
      makeLog({ severity: 9,  body: 'Info message here',  span_id: 'span-001' }),
      makeLog({ severity: 17, body: 'Fatal error occurred', span_id: 'span-002' }),
    ])
    await page.goto('/logs')

    // Both visible initially
    await expect(page.getByText('Info message here')).toBeVisible()
    await expect(page.getByText('Fatal error occurred')).toBeVisible()

    // Click the ERROR chip
    await page.getByRole('button', { name: 'ERROR' }).click()

    // Only the error log should remain visible
    await expect(page.getByText('Fatal error occurred')).toBeVisible()
    await expect(page.getByText('Info message here')).toHaveCount(0)
  })

  test('search box filters logs by body text', async ({ page }) => {
    await stubLogs(page, [
      makeLog({ body: 'Request received from client', span_id: 'span-a' }),
      makeLog({ body: 'Database query executed',      span_id: 'span-b' }),
    ])
    await page.goto('/logs')

    await expect(page.getByText('Request received from client')).toBeVisible()
    await expect(page.getByText('Database query executed')).toBeVisible()

    await page.getByPlaceholder('search logs…').fill('database')

    await expect(page.getByText('Database query executed')).toBeVisible()
    await expect(page.getByText('Request received from client')).toHaveCount(0)
  })

  test('a log with a non-zero trace_id shows the → navigation button', async ({ page }) => {
    const traceId = 'abcdef1234567890abcdef1234567890'
    await stubLogs(page, [
      makeLog({ trace_id: traceId, body: 'traced log entry', span_id: 'span-x' }),
    ])
    await page.goto('/logs')

    // The → button is rendered when hasTrace is true
    const arrowBtn = page.getByRole('button', { name: /→/ })
    await expect(arrowBtn).toBeVisible()

    // Clicking it navigates to /traces/:id
    await arrowBtn.click()
    await expect(page).toHaveURL(new RegExp(`/traces/${traceId}`))
  })
})
