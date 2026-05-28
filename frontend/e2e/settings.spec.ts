import { test, expect, type Route, type Page } from '@playwright/test'
import type { Settings, StorageBreakdown } from '../src/lib/api'

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
    otlp_grpc_port: 4317,
    otlp_http_port: 4318,
    no_browser: false,
    forward: [],
    bind_address_v4: '127.0.0.1',
    bind_address_v6: '::1',
    forward_sample: 1.0,
    runtime: {
      pid: 32118,
      uptime_ns: (4 * 3600 + 12 * 60) * 1_000_000_000, // 4h 12m
      version: '0.4.2',
      config_path: '/home/user/.spaniel/config.yaml',
      otlp_grpc_port: 4317,
      otlp_http_port: 4318,
      db_size_bytes: 612 * 1024 * 1024,
    },
    ...overrides,
  }
}

function makeBreakdown(overrides: Partial<StorageBreakdown> = {}): StorageBreakdown {
  return {
    tables: [
      { name: 'spans',        row_count: 1200, approx_bytes: 512 * 1024 },
      { name: 'logs',         row_count: 300,  approx_bytes: 128 * 1024 },
      { name: 'metrics',      row_count: 80,   approx_bytes: 64 * 1024  },
      { name: 'span_events',  row_count: 50,   approx_bytes: 32 * 1024  },
      { name: 'trace_issues', row_count: 4,    approx_bytes: 4 * 1024   },
    ],
    sessions: [
      { id: 'sess-aaa', label: 'prod-2025-05-27', approx_bytes: 400 * 1024, span_count: 900 },
      { id: 'sess-bbb', label: 'staging',         approx_bytes: 112 * 1024, span_count: 300 },
    ],
    wal_bytes: 0,
    main_bytes: 640 * 1024,
    last_checkpoint_at: 0,
    ...overrides,
  }
}

async function stubChrome(page: Page, breakdown?: StorageBreakdown) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/stats*', r => jsonResponse(r, {
    span_count: 0, trace_count: 0, log_count: 0, db_size: 0,
    session_count: 0, oldest_session_at: 0,
  }))
  await page.route('**/api/forwarders', r => jsonResponse(r, []))
  await page.route('**/api/sessions/active', r => jsonResponse(r, { id: '', label: '' }))
  await page.route('**/api/storage', r => jsonResponse(r, breakdown ?? makeBreakdown()))
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

  test('toggling gRPC off flips the pill to stopped and PUTs otlp_grpc_port=0', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await page.getByLabel('enable grpc receiver').click()

    await expect(page.getByTestId('row-grpc').getByText('stopped')).toBeVisible()
    await expect.poll(() => h.lastPut()).toMatchObject({ otlp_grpc_port: 0 })
  })

  test('changing the retention unit updates the computed-label pill', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings({ retention_days: 7 }))
    await page.goto('/settings')

    await page.getByRole('button', { name: 'Storage', exact: true }).click()

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
    await page.getByRole('button', { name: 'Storage', exact: true }).click()

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
    await page.getByRole('button', { name: 'Storage', exact: true }).click()

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

  test('changing the IPv4 bind address PUTs bind_address_v4', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')

    const input = page.getByLabel('ipv4 bind address')
    await input.fill('0.0.0.0')
    await input.blur()

    await expect.poll(() => h.lastPut()).toMatchObject({ bind_address_v4: '0.0.0.0' })
    await expect(page.getByTestId('row-bind-v4').getByText('all interfaces', { exact: true })).toBeVisible()
  })

  test('changing the IPv6 bind address PUTs bind_address_v6', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')

    const input = page.getByLabel('ipv6 bind address')
    await input.fill('::')
    await input.blur()

    await expect.poll(() => h.lastPut()).toMatchObject({ bind_address_v6: '::' })
    await expect(page.getByTestId('row-bind-v6').getByText('all interfaces', { exact: true })).toBeVisible()
  })

  test('moving the forward-sampling slider updates the percentage pill and PUTs forward_sample', async ({ page }) => {
    await stubChrome(page)
    const h = await stubSettings(page, makeSettings())
    await page.goto('/settings')

    const slider = page.getByLabel('forward sample', { exact: true })
    await slider.evaluate((el: HTMLInputElement) => {
      // React tracks the input's value internally and skips onChange when the
      // value is assigned directly, so go through the prototype setter it hooks.
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
      setter.call(el, '0.5')
      el.dispatchEvent(new Event('input', { bubbles: true }))
      el.dispatchEvent(new Event('change', { bubbles: true }))
    })

    await expect(page.getByTestId('row-forward-sample').getByText('50% of spans')).toBeVisible()
    await expect.poll(() => h.lastPut()).toMatchObject({ forward_sample: 0.5 })
  })

  test('About section shows version + config path', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')

    await page.getByRole('button', { name: 'About' }).click()
    await expect(page.getByText('spaniel 0.4.2')).toBeVisible()
    await expect(page.getByText('/home/user/.spaniel/config.yaml').first()).toBeVisible()
  })

  test('breakdown bar shows segments for each table with correct title attributes', async ({ page }) => {
    const bd = makeBreakdown()
    await stubChrome(page, bd)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Storage', exact: true }).click()

    const bar = page.getByTestId('breakdown-bar')
    await expect(bar).toBeVisible()

    // Each non-trivial segment should render with a descriptive title.
    const spansSegment = page.getByTestId('breakdown-seg-spans')
    await expect(spansSegment).toBeVisible()
    const title = await spansSegment.getAttribute('title')
    expect(title).toMatch(/Spans/)
    expect(title).toMatch(/KB|MB/)

    const logsSegment = page.getByTestId('breakdown-seg-logs')
    await expect(logsSegment).toBeVisible()
  })

  test('breakdown section lists top sessions by size', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Storage', exact: true }).click()

    const sessionsList = page.getByTestId('sessions-breakdown')
    await expect(sessionsList).toBeVisible()
    await expect(sessionsList).toContainText('prod-2025-05-27')
    await expect(sessionsList).toContainText('staging')
  })

  test('compact now button calls /api/settings/compact and shows Done', async ({ page }) => {
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    let compactCalled = 0
    await page.route('**/api/settings/compact', async r => {
      compactCalled++
      return jsonResponse(r, { bytes_before: 640 * 1024, bytes_after: 512 * 1024, reclaimed: 128 * 1024 })
    })
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Storage', exact: true }).click()

    await page.getByTestId('compact-btn').click()
    await expect(page.getByTestId('compact-msg')).toContainText('Done', { timeout: 5000 })
    expect(compactCalled).toBe(1)
  })

  test('check-updates: shows available update when is_outdated', async ({ page }) => {
    // stub POST /api/settings/check-updates
    await page.route('**/api/settings/check-updates', r => r.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ data: {
        current: '0.4.2', latest: '0.5.0', channel: 'stable',
        is_outdated: true, release_notes_url: 'https://github.com/zfogg/spaniel/releases/tag/v0.5.0',
        checked_at_ns: Date.now() * 1_000_000, error: '',
      }, meta: { total: 1, page: 1 } }),
    }))
    await stubChrome(page)
    await stubSettings(page, makeSettings())
    await page.goto('/settings#about')

    await page.getByRole('button', { name: 'About' }).click()
    await page.getByTestId('check-updates-btn').click()
    const result = page.getByTestId('update-result')
    await expect(result).toBeVisible()
    const link = result.getByRole('link')
    await expect(link).toContainText('0.5.0 available')
    await expect(link).toHaveAttribute('href', 'https://github.com/zfogg/spaniel/releases/tag/v0.5.0')
  })
})
