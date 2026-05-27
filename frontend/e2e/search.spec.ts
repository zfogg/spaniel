import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ───────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

async function stubBackend(page: Page, searchResults: unknown[] = []) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/traces*', r => jsonResponse(r, []))
  await page.route('**/api/services', r => jsonResponse(r, []))
  await page.route('**/api/stats*', r => jsonResponse(r, { span_count: 0, trace_count: 0, log_count: 0, db_size: 0 }))
  await page.route('**/api/forwarders', r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
  await page.route('**/api/sessions', r => jsonResponse(r, []))
  await page.route('**/api/lint*', r => jsonResponse(r, []))
  await page.route('**/api/search*', r => jsonResponse(r, searchResults, { total: searchResults.length, page: 1 }))
}

function traceResult(traceId: string, title: string, subtitle = 'my-svc') {
  return { kind: 'trace', trace_id: traceId, title, subtitle, session_id: 's1' }
}

function logResult(traceId: string, body: string, subtitle = 'my-svc') {
  return { kind: 'log', trace_id: traceId, span_id: 'sp1', title: body, subtitle, session_id: 's1' }
}

// ── opening the palette ───────────────────────────────────────────────────────

test.describe('Command palette', () => {
  test('opens with Ctrl+K', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).toBeVisible()
  })

  test('opens with the / shortcut', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    // Make sure focus is not in an input before pressing /.
    await page.locator('body').click()
    await page.keyboard.press('/')
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).toBeVisible()
  })

  test('opens via the Search button in the nav bar', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.getByRole('button', { name: /search/i }).click()
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).toBeVisible()
  })

  // ── closing ─────────────────────────────────────────────────────────────────

  test('closes with Escape', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).not.toBeVisible()
  })

  test('closes when clicking the backdrop overlay', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).toBeVisible()
    // Click outside the dialog (top-left corner of the overlay).
    await page.mouse.click(10, 10)
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).not.toBeVisible()
  })

  // ── footer hints ─────────────────────────────────────────────────────────────

  test('shows keyboard hint footer', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    // The footer renders kbd+label with no whitespace: "↑↓navigate", "↵open", "Escclose".
    await expect(page.getByText('↑↓navigate')).toBeVisible()
    await expect(page.getByText('↵open')).toBeVisible()
    await expect(page.getByText('Escclose')).toBeVisible()
  })

  // ── search results ───────────────────────────────────────────────────────────

  test('displays trace results returned by the API', async ({ page }) => {
    await stubBackend(page, [
      traceResult('trace-abc', 'GET /api/orders'),
      traceResult('trace-def', 'POST /api/checkout'),
    ])
    await page.goto('/')
    await page.keyboard.press('Control+k')

    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('orders')

    await expect(page.getByText('GET /api/orders')).toBeVisible()
    await expect(page.getByText('POST /api/checkout')).toBeVisible()
  })

  test('displays log results with a "log" kind badge', async ({ page }) => {
    await stubBackend(page, [
      logResult('trace-log', 'database connection timed out'),
    ])
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('timeout')

    await expect(page.getByText('database connection timed out')).toBeVisible()
    // Kind badge is a separate element with exact text "log".
    await expect(page.locator('[class*="rounded"][class*="border"]').filter({ hasText: /^log$/ })).toBeVisible()
  })

  test('shows "no results" message when API returns empty', async ({ page }) => {
    await stubBackend(page, [])
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('xyzzy-nonexistent')

    await expect(page.getByText(/No results for/)).toBeVisible()
    await expect(page.getByText('xyzzy-nonexistent')).toBeVisible()
  })

  // ── navigation ───────────────────────────────────────────────────────────────

  test('clicking a trace result navigates to /traces/:id', async ({ page }) => {
    await stubBackend(page, [traceResult('trace-nav', 'GET /orders')])
    await page.route('**/api/traces/trace-nav', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('orders')
    await page.getByText('GET /orders').click()

    await expect(page).toHaveURL(/\/traces\/trace-nav$/)
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).not.toBeVisible()
  })

  test('clicking a log result navigates to /logs', async ({ page }) => {
    await stubBackend(page, [logResult('trace-log', 'payment failed')])
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('payment')
    await page.getByText('payment failed').click()

    await expect(page).toHaveURL(/\/logs$/)
    await expect(page.getByPlaceholder('Search traces, spans, services, logs…')).not.toBeVisible()
  })

  test('Enter on the selected result navigates', async ({ page }) => {
    await stubBackend(page, [traceResult('trace-enter', 'PUT /resource')])
    await page.route('**/api/traces/trace-enter', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('resource')
    await expect(page.getByText('PUT /resource')).toBeVisible()
    await page.keyboard.press('Enter')

    await expect(page).toHaveURL(/\/traces\/trace-enter$/)
  })

  // ── keyboard navigation ───────────────────────────────────────────────────────

  test('ArrowDown moves selection to second result, Enter activates it', async ({ page }) => {
    await stubBackend(page, [
      traceResult('trace-first', 'GET /first'),
      traceResult('trace-second', 'GET /second'),
    ])
    await page.route('**/api/traces/trace-second', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('get')
    await expect(page.getByText('GET /first')).toBeVisible()
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Enter')

    await expect(page).toHaveURL(/\/traces\/trace-second$/)
  })

  // ── recent searches ───────────────────────────────────────────────────────────

  test('shows recent searches when palette reopens with no query', async ({ page }) => {
    await stubBackend(page, [traceResult('trace-recent', 'GET /recent')])
    await page.route('**/api/traces/trace-recent', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))
    await page.goto('/')

    // Make a search and activate a result to save it to recents.
    await page.keyboard.press('Control+k')
    await page.getByPlaceholder('Search traces, spans, services, logs…').fill('recent')
    await expect(page.getByText('GET /recent')).toBeVisible()
    await page.getByText('GET /recent').click()

    // Reopen — should show 'recent' as a saved query in the list.
    await page.keyboard.press('Control+k')
    // The query "recent" appears as the item text; the label "recent" appears right-aligned.
    // Two occurrences of "recent" expected: the query text and the kind label.
    await expect(page.getByText('recent').first()).toBeVisible()
  })
})
