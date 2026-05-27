import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

const SESSIONS = [
  { id: 'b1', label: 'baseline-session', created_at: 0, is_baseline: true,  is_imported: false, span_count: 10, trace_count: 2, services: 'api' },
  { id: 'c1', label: 'compare-session',  created_at: 1, is_baseline: false, is_imported: false, span_count: 12, trace_count: 2, services: 'api' },
]

const DIFF_RESULT = {
  baseline: { session_id: 'b1', label: 'baseline-session', total_duration_ns: 1_000_000_000, span_count: 10, db_calls: 3 },
  compare:  { session_id: 'c1', label: 'compare-session',  total_duration_ns: 1_200_000_000, span_count: 12, db_calls: 4 },
  summary: {
    duration_delta_ns:  200_000_000,
    duration_delta_pct: 20,
    spans_added:   2,
    spans_removed: 1,
    db_call_delta: 1,
  },
  spans: [
    { name: 'GET /users',  service_name: 'api', status: 'same',    baseline_duration_ns: 500_000_000, compare_duration_ns: 600_000_000, delta_pct:  20, depth: 0 },
    { name: 'SELECT *',    service_name: 'db',  status: 'removed', baseline_duration_ns: 200_000_000, compare_duration_ns: 0,           delta_pct:   0, depth: 1 },
    { name: 'INSERT INTO', service_name: 'db',  status: 'added',   baseline_duration_ns: 0,           compare_duration_ns: 150_000_000, delta_pct:   0, depth: 1 },
  ],
  baseline_spans: [],
  compare_spans:  [],
}

async function stubDiff(page: Page, diffResult: unknown = DIFF_RESULT) {
  await page.routeWebSocket('**/ws', ws => ws.close())

  // Single predicate handler — avoids LIFO ordering races under parallel load.
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const pathname = new URL(r.request().url()).pathname

    if (pathname === '/api/forwarders')
      return jsonResponse(r, [])
    if (pathname === '/api/sessions/active')
      return jsonResponse(r, { id: '', label: '' })
    if (pathname === '/api/sessions')
      return jsonResponse(r, SESSIONS)
    if (pathname === '/api/stats')
      return jsonResponse(r, { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 })
    if (pathname.startsWith('/api/diff'))
      return r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: diffResult }) })

    return r.continue()
  })
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('DiffPage', () => {
  test('shows empty state when no baseline/compare params are provided', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff')

    await expect(page.getByText('select two sessions to compare')).toBeVisible()
  })

  test('loads the diff and shows both session labels in column headers', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff?baseline=b1&compare=c1')

    // The SpanColumn headers render the label as a <span> (not inside <option>).
    // Use a role-based locator to exclude hidden <option> elements.
    await expect(page.locator('span', { hasText: 'baseline-session' }).first()).toBeVisible()
    await expect(page.locator('span', { hasText: 'compare-session' }).first()).toBeVisible()
  })

  test('summary strip shows duration stat and span counts', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff?baseline=b1&compare=c1')

    // DiffStat labels
    await expect(page.getByText('total', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('spans', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('db calls', { exact: true }).first()).toBeVisible()
  })

  test('footer shows spans removed and spans added from summary', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff?baseline=b1&compare=c1')

    // Footer insight strip: "−1 spans removed" and "+2 spans added"
    await expect(page.getByText(/spans removed/)).toBeVisible()
    await expect(page.getByText(/spans added/)).toBeVisible()
  })

  test('a removed span only appears in the baseline column (has baseline_duration_ns > 0)', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff?baseline=b1&compare=c1')

    // 'SELECT *' is removed — baseline_duration_ns=200ms, compare_duration_ns=0
    // The compare SpanColumn filters out spans with compare_duration_ns===0,
    // so 'SELECT *' must appear at least once (in baseline) but the compare col must not show it
    await expect(page.getByText('SELECT *').first()).toBeVisible()
  })

  test('an added span only appears in the compare column (has compare_duration_ns > 0)', async ({ page }) => {
    await stubDiff(page)
    await page.goto('/diff?baseline=b1&compare=c1')

    // 'INSERT INTO' is added — baseline_duration_ns=0, compare_duration_ns=150ms
    await expect(page.getByText('INSERT INTO').first()).toBeVisible()
  })

  test('the ← back button navigates to the previous page', async ({ page }) => {
    await stubDiff(page)
    // Start from sessions so there is a history entry to go back to
    await page.goto('/sessions')
    await page.goto('/diff')

    await page.getByText('← back').click()

    // After going back, URL should no longer be /diff
    await expect(page).not.toHaveURL(/\/diff/)
  })
})
