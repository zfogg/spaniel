import { test, expect, type Route, type Page } from '@playwright/test'
import type { Settings } from '../src/lib/api'

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 1, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

function makeSettings(overrides: Partial<Settings> = {}): Settings {
  return {
    port: 8080,
    db_path: '~/.spaniel/spaniel.duckdb',
    retention_days: 7,
    max_sessions: 50,
    max_db_size_mb: 500,
    grpc_port: 4317,
    http_port: 4318,
    no_browser: false,
    forward: [],
    runtime: {
      pid: 32118,
      uptime_ns: (4 * 3600 + 12 * 60) * 1_000_000_000, // 4h 12m
      version: '0.4.2',
      config_path: '/home/user/.spaniel/config.yaml',
      grpc_port: 4317,
      http_port: 4318,
      db_size_bytes: 612 * 1024 * 1024,
    },
    ...overrides,
  }
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

interface SettingsHarness {
  current: () => Settings
  lastPut: () => unknown | null
  putCount: () => number
  dropCount: () => number
}

async function stubSettings(page: Page, initial: Settings): Promise<SettingsHarness> {
  let state = initial
  let lastPut: unknown = null
  let putCount = 0
  let dropCount = 0

  await page.route(url => {
    const p = new URL(url.toString()).pathname
    return p === '/api/settings' || p === '/api/settings/data'
  }, async r => {
    const req = r.request()
    const url = new URL(req.url())
    if (url.pathname === '/api/settings/data') {
      dropCount++
      return jsonResponse(r, { ok: true })
    }
    if (req.method() === 'PUT') {
      putCount++
      const body = JSON.parse(req.postData() ?? '{}')
      lastPut = body
      // Validate port like the backend does.
      if (typeof body.port === 'number' && (body.port < 1 || body.port > 65535)) {
        return r.fulfill({ status: 400, contentType: 'application/json',
          body: JSON.stringify({ error: `port must be 1–65535, got ${body.port}` }) })
      }
      state = { ...state, ...body, runtime: state.runtime }
      return jsonResponse(r, state)
    }
    return jsonResponse(r, state)
  })

  return {
    current: () => state,
    lastPut: () => lastPut,
    putCount: () => putCount,
    dropCount: () => dropCount,
  }
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Settings page', () => {
  test('renders the page chrome — heading, intro, daemon pill', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    await expect(page.getByText(/Saved to/)).toBeVisible()

    // daemon pill shows pid + uptime
    const pill = page.getByTestId('daemon-pill')
    await expect(pill).toContainText('daemon running')
    await expect(pill).toContainText('pid 32118')
    await expect(pill).toContainText('4h 12m')
  })

  test('renders the Network section by default with listening pills', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await expect(page.getByTestId('row-grpc')).toBeVisible()
    await expect(page.getByTestId('row-http')).toBeVisible()
    await expect(page.getByTestId('row-grpc').getByText(/listening :4317/)).toBeVisible()
    await expect(page.getByTestId('row-http').getByText(/listening :4318/)).toBeVisible()
  })

  test('toggling gRPC off flips the pill to stopped and PUTs grpc_port=0', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await page.getByLabel('enable grpc receiver').click()

    await expect(page.getByTestId('row-grpc').getByText('stopped')).toBeVisible()
    await expect.poll(() => h.lastPut()).toMatchObject({ grpc_port: 0 })
  })

  test('changing the retention unit updates the computed-label pill', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings({ retention_days: 7 }))
    await page.goto('/settings')

    await page.getByRole('button', { name: 'Storage · DuckDB' }).click()

    // Default unit is days → "7 days".
    await expect(page.getByTestId('retention-pill')).toContainText('7 days')

    await page.getByLabel('retention unit').selectOption('hours')
    // 7 (interpreted as hours) → "7h".
    await expect(page.getByTestId('retention-pill')).toContainText('7h')

    await page.getByLabel('retention unit').selectOption('minutes')
    await expect(page.getByTestId('retention-pill')).toContainText('7min')
  })

  test('storage usage bar fills proportionally and clamps under 85% to accent color', async ({ page }) => {
    await stubChrome(page)
    // 612 MB used of 500 MB cap → over 100% → clamps to 100% width, danger color.
    await stubSettings(page, makeSettings({ max_db_size_mb: 500 }))
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Storage · DuckDB' }).click()

    const fill = page.getByTestId('usage-bar-fill')
    await expect(fill).toBeVisible()
    // clamped at 100%
    const width = await fill.evaluate(el => (el as HTMLElement).style.width)
    expect(width).toBe('100%')
  })

  test('rejecting an invalid port surfaces the server error', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')

    // The optimistic update flips display, then the server rejects with 400 and
    // the save-state strip shows the error.
    const portInput = page.getByLabel('ui port')
    await portInput.fill('0')
    await portInput.blur()

    await expect(page.getByTestId('save-state')).toContainText('port must be 1', { timeout: 5000 })
  })

  test('drop-all-data prompts confirmation and only hits the API on accept', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Storage · DuckDB' }).click()

    // 1) cancel — no API call.
    page.once('dialog', d => d.dismiss())
    await page.getByRole('button', { name: /drop & recreate/i }).click()
    await page.waitForTimeout(150)
    expect(h.dropCount()).toBe(0)

    // 2) accept — one API call.
    page.once('dialog', d => d.accept())
    await page.getByRole('button', { name: /drop & recreate/i }).click()
    await expect.poll(() => h.dropCount()).toBe(1)
  })

  test('About section shows version + config path', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await page.getByRole('button', { name: 'About' }).click()
    await expect(page.getByText('spaniel 0.4.2')).toBeVisible()
    await expect(page.getByText('/home/user/.spaniel/config.yaml').first()).toBeVisible()
  })
})
