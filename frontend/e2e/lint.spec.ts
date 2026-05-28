import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface LintWarning {
  span_id: string
  trace_id: string
  session_id: string
  rule_id: string
  message: string
  severity: string
  created_at: number
}

interface TraceIssue {
  id: string; trace_id: string; session_id: string
  kind: string; fingerprint: string
  count: number; wasted_ns: number
  parent_span_id: string; example_span_id: string
  created_at: number
}

async function stubLint(page: Page, warnings: LintWarning[], issues: TraceIssue[] = []) {
  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/'), r => {
    const pathname = new URL(r.request().url()).pathname
    if (pathname === '/api/stats')
      return jsonResponse(r, { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 0, oldest_session_at: 0 })
    if (pathname === '/api/forwarders') return jsonResponse(r, [])
    if (pathname === '/api/sessions/active') return jsonResponse(r, { id: '', label: '' })
    if (pathname.startsWith('/api/lint')) return jsonResponse(r, warnings)
    if (pathname === '/api/issues') return jsonResponse(r, issues)
    return r.continue()
  })
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('LintPage', () => {
  test('shows empty state when there are no warnings', async ({ page }) => {
    await stubLint(page, [])
    await page.goto('/lint')

    await expect(page.getByText('All clean!')).toBeVisible()
  })

  test('a warning row shows rule_id, severity badge, and message', async ({ page }) => {
    await stubLint(page, [
      {
        span_id:    'aaaa1111',
        trace_id:   'tttt1111',
        session_id: 's1',
        rule_id:    'missing.db.system',
        message:    'db.system attribute is required for database spans',
        severity:   'warning',
        created_at: 0,
      },
    ])
    await page.goto('/lint')

    await expect(page.getByText('missing.db.system')).toBeVisible()
    await expect(page.getByText('warning').first()).toBeVisible()
    await expect(page.getByText('db.system attribute is required for database spans')).toBeVisible()
  })

  test('error count badge appears in the header when there are error-severity warnings', async ({ page }) => {
    await stubLint(page, [
      {
        span_id:    'aaaa2222',
        trace_id:   'tttt2222',
        session_id: 's1',
        rule_id:    'required.attr',
        message:    'Required attribute is missing',
        severity:   'error',
        created_at: 0,
      },
    ])
    await page.goto('/lint')

    // The header renders "1 error" badge when errors > 0
    await expect(page.getByText(/1 error/)).toBeVisible()
  })

  test('warn count badge appears in the header when there are warning-severity warnings', async ({ page }) => {
    await stubLint(page, [
      {
        span_id:    'aaaa3333',
        trace_id:   'tttt3333',
        session_id: 's1',
        rule_id:    'semconv.warning',
        message:    'Consider adding rpc.method attribute',
        severity:   'warning',
        created_at: 0,
      },
    ])
    await page.goto('/lint')

    // The header renders "1 warn" badge when warnCnt > 0
    await expect(page.getByText(/1 warn/)).toBeVisible()
  })

  test('a warning with a trace_id shows a trace link', async ({ page }) => {
    await stubLint(page, [
      {
        span_id:    'span-abc123',
        trace_id:   'trace-deadbeef-cafebabe',
        session_id: 's1',
        rule_id:    'n_plus_one',
        message:    'N+1 query pattern detected',
        severity:   'warning',
        created_at: 0,
      },
    ])
    await page.goto('/lint')

    // LintRow renders "trace → <truncated id>" as a link when trace_id is present
    await expect(page.getByText(/trace →/)).toBeVisible()
    await expect(page.locator('a[href*="/traces/trace-deadbeef-cafebabe"]')).toBeVisible()
  })

  test('summary strip shows one card per detector kind from /api/issues', async ({ page }) => {
    const issues: TraceIssue[] = [
      { id: 'i1', trace_id: 't1', session_id: 's1', kind: 'slow_db',     fingerprint: 'SELECT reports', count: 1, wasted_ns: 100_000_000, parent_span_id: '', example_span_id: 'ex1', created_at: 0 },
      { id: 'i2', trace_id: 't2', session_id: 's1', kind: 'chatty_http', fingerprint: 'api.example.com', count: 7, wasted_ns: 0,           parent_span_id: '', example_span_id: 'ex2', created_at: 0 },
      { id: 'i3', trace_id: 't3', session_id: 's1', kind: 'tracing_gap', fingerprint: 'POST /charge',   count: 1, wasted_ns: 300_000_000, parent_span_id: '', example_span_id: 'ex3', created_at: 0 },
    ]
    await stubLint(page, [], issues)
    await page.goto('/lint')

    await expect(page.getByText('slow DB')).toBeVisible()
    await expect(page.getByText('chatty HTTP')).toBeVisible()
    await expect(page.getByText('tracing gap')).toBeVisible()
  })

  test('summary strip shows one card per distinct kind, collapsing duplicates', async ({ page }) => {
    const issues: TraceIssue[] = [
      { id: 'a1', trace_id: 'ta', session_id: 's1', kind: 'error_chain',    fingerprint: 'payment', count: 2, wasted_ns: 0, parent_span_id: '', example_span_id: 'ea', created_at: 0 },
      { id: 'b1', trace_id: 'tb', session_id: 's1', kind: 'error_chain',    fingerprint: 'auth',    count: 1, wasted_ns: 0, parent_span_id: '', example_span_id: 'eb', created_at: 0 },
      { id: 'c1', trace_id: 'tc', session_id: 's1', kind: 'serial_promise', fingerprint: 'dashboard', count: 4, wasted_ns: 200_000_000, parent_span_id: '', example_span_id: 'ec', created_at: 0 },
    ]
    await stubLint(page, [], issues)
    await page.goto('/lint')

    await expect(page.getByText('error chain')).toBeVisible()
    await expect(page.getByText('serial promises')).toBeVisible()
    await expect(page.getByText('error chain')).toHaveCount(1)
  })
})
