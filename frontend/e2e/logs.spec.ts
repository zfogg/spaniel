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
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const pathname = new URL(r.request().url()).pathname
    if (pathname === '/api/stats')
      return jsonResponse(r, { span_count: 0, trace_count: 0, log_count: logs.length, db_size: 0, session_count: 0, oldest_session_at: 0 })
    if (pathname === '/api/forwarders') return jsonResponse(r, [])
    if (pathname === '/api/sessions/active') return jsonResponse(r, { id: '', label: '' })
    if (pathname === '/api/services') return jsonResponse(r, services)
    if (pathname.startsWith('/api/logs')) return jsonResponse(r, logs)
    return r.continue()
  })
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('LogViewer', () => {
  test('shows empty state when there are no logs', async ({ page }) => {
    await stubLogs(page, [])
    await page.goto('/logs')

    await expect(page.getByText('No logs yet')).toBeVisible()
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
    await page.getByRole('button', { name: 'ERROR', exact: true }).click()

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
    const arrowBtn = page.getByRole('button', { name: '→', exact: true })
    await expect(arrowBtn).toBeVisible()

    // Clicking it navigates to /traces/:id with a ?spanId deep-link.
    await arrowBtn.click()
    await expect(page).toHaveURL(new RegExp(`/traces/${traceId}\\?spanId=span-x`))
  })

  test('clicking a row opens the inspector with body, attrs, and Open trace', async ({ page }) => {
    const traceId = 'abcdef1234567890abcdef1234567890'
    await stubLogs(page, [
      makeLog({
        trace_id: traceId, span_id: 'span-x', severity: 17,
        service_name: 'pricing', body: 'connection refused: dial tcp 127.0.0.1:5432',
        attributes: JSON.stringify({ 'error.kind': 'NetworkError', retry: '3' }),
      }),
    ])
    await page.goto('/logs')

    await page.getByText('connection refused: dial tcp 127.0.0.1:5432').click()

    // Inspector landmark + content.
    const inspector = page.getByRole('complementary', { name: 'Log details' })
    await expect(inspector).toBeVisible()
    await expect(inspector.getByText('connection refused: dial tcp 127.0.0.1:5432')).toBeVisible()
    await expect(inspector.getByText('error.kind')).toBeVisible()
    await expect(inspector.getByText('NetworkError')).toBeVisible()
    await expect(inspector.getByText('ERROR').first()).toBeVisible()

    // Open-trace button navigates with the spanId query param.
    await inspector.getByRole('button', { name: /Open trace/ }).click()
    await expect(page).toHaveURL(new RegExp(`/traces/${traceId}\\?spanId=span-x`))
  })

  test('Esc closes the inspector', async ({ page }) => {
    await stubLogs(page, [
      makeLog({ body: 'click me to open the inspector' }),
    ])
    await page.goto('/logs')

    await page.getByText('click me to open the inspector').click()
    await expect(page.getByRole('complementary', { name: 'Log details' })).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByRole('complementary', { name: 'Log details' })).toHaveCount(0)
  })

  test('severity chips are auto-generated from the present severities', async ({ page }) => {
    // Only INFO + ERROR present — DEBUG/WARN/FATAL/TRACE chips should not show.
    await stubLogs(page, [
      makeLog({ severity: 9,  body: 'info one' }),
      makeLog({ severity: 17, body: 'error one' }),
    ])
    await page.goto('/logs')

    await expect(page.getByRole('button', { name: 'ALL',   exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'INFO',  exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'ERROR', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'DEBUG', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'WARN',  exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'FATAL', exact: true })).toHaveCount(0)
  })

  test('a WebSocket log event refreshes the list and shows the new log', async ({ page }) => {
    // Live updates are now driven by TanStack Query: a 'log' WS event triggers a
    // (throttled) invalidation of the 'logs' query, which re-fetches /api/logs.
    // So the server-side list is what surfaces — we flip it once the event fires.
    const liveLog = {
      timestamp_ns: Date.now() * 1_000_000,
      trace_id: '0000000000000000',
      span_id: 'span-ws-001',
      severity: 9,
      body: 'live log from websocket',
      attributes: '{}',
      service_name: 'ws-test-svc',
      session_id: 's1',
      received_at: Date.now() * 1_000_000,
    }
    let logs: unknown[] = []

    // Multiple WS connections are created per page (BottomBar × 2, LogViewer's
    // central invalidator × 1), so collect all of them and broadcast to each.
    const serverWsList: import('@playwright/test').WebSocketRoute[] = []
    await page.routeWebSocket('**/ws', ws => {
      serverWsList.push(ws)
    })
    await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
      const pathname = new URL(r.request().url()).pathname
      if (pathname === '/api/stats')
        return jsonResponse(r, { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 })
      if (pathname === '/api/forwarders') return jsonResponse(r, [])
      if (pathname === '/api/sessions/active') return jsonResponse(r, { id: '', label: '' })
      if (pathname === '/api/services') return jsonResponse(r, [])
      if (pathname.startsWith('/api/logs')) return jsonResponse(r, logs)
      return r.continue()
    })

    await page.goto('/logs')
    await expect(page.getByText('No logs yet')).toBeVisible()

    // The log now exists server-side; pushing the WS event drives the refetch.
    logs = [liveLog]
    const logEvent = {
      type: 'log',
      timestamp_ns: liveLog.timestamp_ns,
      payload: {
        traceId: liveLog.trace_id,
        spanId: liveLog.span_id,
        severity: liveLog.severity,
        body: liveLog.body,
        serviceName: liveLog.service_name,
        sessionId: liveLog.session_id,
      },
    }
    for (const ws of serverWsList) {
      try { ws.send(JSON.stringify(logEvent)) } catch { /* connection may be closed */ }
    }

    await expect(page.getByText('live log from websocket')).toBeVisible()
  })
})
