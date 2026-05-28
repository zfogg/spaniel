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

// expectPaletteOpen asserts the palette is *actually* on top of the page,
// not just present in the DOM. Plain `.toBeVisible()` is a trap here because
// cmdk's portal mounts the unstyled <input> at body-bottom even when the
// dialog has no positioning CSS — Playwright returns true on bounding box,
// but a real user sees nothing. We check:
//   1. The cmdk Dialog content (`[cmdk-dialog]`) is `position: fixed` —
//      the regression we hit was cmdk shipping zero CSS, so without our
//      contentClassName the content laid out as static at body bottom.
//   2. It has a positive z-index so it overlays the rest of the chrome.
//   3. Its bounding box has real dimensions and sits inside the viewport.
//   4. The overlay backdrop (`[cmdk-overlay]`) also exists and is fixed.
async function expectPaletteOpen(page: Page) {
  const content = page.locator('[cmdk-dialog]')
  const overlay = page.locator('[cmdk-overlay]')
  await expect(content).toBeVisible()
  await expect(overlay).toBeVisible()

  const contentStyle = await content.evaluate((el) => {
    const cs = getComputedStyle(el as HTMLElement)
    const r = (el as HTMLElement).getBoundingClientRect()
    return { position: cs.position, zIndex: parseInt(cs.zIndex, 10) || 0, width: r.width, height: r.height, top: r.top, left: r.left }
  })
  expect(contentStyle.position, 'palette content must be position:fixed').toBe('fixed')
  expect(contentStyle.zIndex, 'palette content must have a positive z-index').toBeGreaterThan(0)
  expect(contentStyle.width,  'palette content must have nonzero width').toBeGreaterThan(200)
  expect(contentStyle.height, 'palette content must have nonzero height').toBeGreaterThan(50)

  const overlayStyle = await overlay.evaluate((el) => getComputedStyle(el as HTMLElement).position)
  expect(overlayStyle, 'palette overlay must be position:fixed').toBe('fixed')

  // Bounding box sits inside the viewport (sanity that we didn't position
  // off-screen with a typo).
  const vp = page.viewportSize() ?? { width: 1280, height: 720 }
  expect(contentStyle.top,  'content top within viewport').toBeGreaterThanOrEqual(0)
  expect(contentStyle.top,  'content top within viewport').toBeLessThan(vp.height)
  expect(contentStyle.left, 'content left within viewport').toBeGreaterThanOrEqual(0)
  expect(contentStyle.left, 'content left within viewport').toBeLessThan(vp.width)
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
    await expectPaletteOpen(page)
  })

  test('opens with the / shortcut', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    // Make sure focus is not in an input before pressing /.
    await page.locator('body').click()
    await page.keyboard.press('/')
    await expectPaletteOpen(page)
  })

  test('opens via the Search button in the nav bar', async ({ page }) => {
    // The Search button living in the nav is a static piece of chrome — if
    // _everything else_ palette-related broke this would still pass, which is
    // why we keep it as a separate "the button is in the nav" assertion.
    await stubBackend(page)
    await page.goto('/')
    await expect(page.getByRole('button', { name: /search/i })).toBeVisible()
    await page.getByRole('button', { name: /search/i }).click()
    await expectPaletteOpen(page)
  })

  test('opens above the page chrome (z-index sanity)', async ({ page }) => {
    // Regression guard for the bug where cmdk renders the dialog with no
    // CSS — the input was in the DOM but the user saw nothing. We click
    // through to the input and confirm focus actually landed.
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    const input = page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…')
    await expect(input).toBeFocused()
  })

  // ── closing ─────────────────────────────────────────────────────────────────

  test('closes with Escape', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    await page.keyboard.press('Escape')
    await expect(page.locator('[cmdk-dialog]')).toHaveCount(0)
  })

  test('closes when clicking the backdrop overlay', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    // Click the overlay element by name — the unstyled cmdk had no overlay
    // at coordinates (10,10), so the original positional click was a false
    // positive on the unstyled palette.
    await page.locator('[cmdk-overlay]').click({ position: { x: 4, y: 4 } })
    await expect(page.locator('[cmdk-dialog]')).toHaveCount(0)
  })

  // ── footer hints ─────────────────────────────────────────────────────────────

  test('shows keyboard hint footer', async ({ page }) => {
    await stubBackend(page)
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    // The footer renders kbd+label with no whitespace: "↑↓navigate", "↵open", "Escclose".
    const dialog = page.locator('[cmdk-dialog]')
    await expect(dialog.getByText('↑↓navigate')).toBeVisible()
    await expect(dialog.getByText('↵open')).toBeVisible()
    await expect(dialog.getByText('Escclose')).toBeVisible()
  })

  // ── search results ───────────────────────────────────────────────────────────

  test('displays trace results returned by the API', async ({ page }) => {
    await stubBackend(page, [
      traceResult('trace-abc', 'GET /api/orders'),
      traceResult('trace-def', 'POST /api/checkout'),
    ])
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)

    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('orders')

    await expect(page.getByText('GET /api/orders')).toBeVisible()
    await expect(page.getByText('POST /api/checkout')).toBeVisible()
  })

  test('displays log results with a "log" kind badge', async ({ page }) => {
    await stubBackend(page, [
      logResult('trace-log', 'database connection timed out'),
    ])
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('timeout')

    await expect(page.getByText('database connection timed out')).toBeVisible()
    // Kind badge is a separate element with exact text "log".
    await expect(page.locator('[class*="rounded"][class*="border"]').filter({ hasText: /^log$/ })).toBeVisible()
  })

  test('shows "no results" message when API returns empty', async ({ page }) => {
    await stubBackend(page, [])
    await page.goto('/')
    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('xyzzy-nonexistent')

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
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('orders')
    await page.getByText('GET /orders').click()

    await expect(page).toHaveURL(/\/traces\/trace-nav$/)
    await expect(page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…')).not.toBeVisible()
  })

  test('clicking a log result navigates to /logs', async ({ page }) => {
    await stubBackend(page, [logResult('trace-log', 'payment failed')])
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('payment')
    await page.getByText('payment failed').click()

    await expect(page).toHaveURL(/\/logs$/)
    await expect(page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…')).not.toBeVisible()
  })

  test('Enter on the selected result navigates', async ({ page }) => {
    await stubBackend(page, [traceResult('trace-enter', 'PUT /resource')])
    await page.route('**/api/traces/trace-enter', r => jsonResponse(r, []))
    await page.route('**/api/issues*', r => jsonResponse(r, []))
    await page.goto('/')

    await page.keyboard.press('Control+k')
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('resource')
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
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('get')
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
    await expectPaletteOpen(page)
    await page.getByPlaceholder('Search traces, spans, sessions, services, lints, logs…').fill('recent')
    await expect(page.getByText('GET /recent')).toBeVisible()
    await page.getByText('GET /recent').click()

    // Reopen — should show 'recent' as a saved query in the list.
    await page.keyboard.press('Control+k')
    // The query "recent" appears as the item text; the label "recent" appears right-aligned.
    // Two occurrences of "recent" expected: the query text and the kind label.
    await expect(page.getByText('recent').first()).toBeVisible()
  })
})
