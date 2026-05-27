import { test, expect, type Route, type Page } from '@playwright/test'

// ── helpers ──────────────────────────────────────────────────────────────────

function jsonResponse(route: Route, data: unknown, meta: Record<string, unknown> = { total: 0, page: 1 }) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ data, meta }),
  })
}

interface SessionFixture {
  id: string
  label: string
  created_at: number
  is_baseline: boolean
  is_imported: boolean
  span_count: number
  trace_count: number
  services: string
}

function makeSession(overrides: Partial<SessionFixture> & { id: string; label: string }): SessionFixture {
  return {
    created_at: Date.now() * 1_000_000,
    is_baseline: false,
    is_imported: false,
    span_count: 0,
    trace_count: 0,
    services: '',
    ...overrides,
  }
}

interface StubOptions {
  sessions: SessionFixture[]
  activeId?: string
}

/**
 * Wire up all routes the Sessions page (and its shared layout) needs.
 *
 * All session-related routing goes through a single URL-predicate handler so
 * we never have ordering/priority surprises between overlapping globs.
 * The deletedIds set is mutated by the DELETE handler and honoured on the next
 * list refetch — simulating the server removing the row.
 */
async function stubBackend(
  page: Page,
  opts: StubOptions,
  callbacks: {
    onBaseline?: (id: string) => void
    onActivate?: (id: string) => void
    onDelete?: (id: string) => void
  } = {}
) {
  const deletedIds = new Set<string>()

  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/forwarders', r => jsonResponse(r, []))

  const activeId = opts.activeId ?? (opts.sessions[0]?.id ?? '')

  // Single predicate handler for everything under /api/sessions*.
  // Playwright LIFO means this must be registered AFTER the forwarders stub
  // but since we use a function predicate it won't collide with the glob.
  await page.route(url => new URL(url.toString()).pathname.startsWith('/api/sessions'), r => {
    const urlObj = new URL(r.request().url())
    const pathname = urlObj.pathname          // e.g. /api/sessions, /api/sessions/active, /api/sessions/id/activate
    const method = r.request().method()
    const segments = pathname.split('/').filter(Boolean) // ['api', 'sessions', id?, action?]
    const id = segments[2]
    const action = segments[3]

    // GET /api/sessions/active — must be checked before the generic /:id branch
    if (pathname === '/api/sessions/active') {
      const activeSession = opts.sessions.find(s => s.id === activeId)
      return jsonResponse(r, { id: activeId, label: activeSession?.label ?? '' })
    }

    // GET/POST /api/sessions (list / create)
    if (pathname === '/api/sessions') {
      if (method === 'POST') {
        return jsonResponse(r, makeSession({ id: 'new-id', label: 'new session' }))
      }
      // GET — return list minus deleted
      return jsonResponse(r, opts.sessions.filter(s => !deletedIds.has(s.id)))
    }

    // POST /api/sessions/:id/activate
    if (action === 'activate' && method === 'POST') {
      callbacks.onActivate?.(id)
      const sess = opts.sessions.find(s => s.id === id)
      return jsonResponse(r, sess ?? makeSession({ id, label: id }))
    }

    // POST /api/sessions/:id/baseline
    if (action === 'baseline' && method === 'POST') {
      callbacks.onBaseline?.(id)
      return jsonResponse(r, { ok: true })
    }

    // DELETE /api/sessions/:id
    if (!action && method === 'DELETE') {
      deletedIds.add(id)
      callbacks.onDelete?.(id)
      return jsonResponse(r, { ok: true })
    }

    // GET /api/sessions/:id
    const sess = opts.sessions.find(s => s.id === id)
    if (sess) return jsonResponse(r, sess)
    return r.fulfill({ status: 404, body: 'not found' })
  })
}

// ── specs ────────────────────────────────────────────────────────────────────

test.describe('Sessions page', () => {
  test('shows empty state when no sessions exist', async ({ page }) => {
    await stubBackend(page, { sessions: [], activeId: '' })
    await page.goto('/sessions')

    await expect(page.getByText('no sessions yet')).toBeVisible()
    // CompareBar always present — shows "— pick baseline —" placeholder
    await expect(page.getByText('— pick baseline —')).toBeVisible()
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeDisabled()
  })

  test('lists sessions with label, trace_count, span_count', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'alpha-session', trace_count: 12, span_count: 48 }),
      makeSession({ id: 'sess-2', label: 'beta-session', trace_count: 7, span_count: 21 }),
    ]
    await stubBackend(page, { sessions, activeId: 'sess-1' })
    await page.goto('/sessions')

    await expect(page.getByText('alpha-session').first()).toBeVisible()
    await expect(page.getByText('beta-session').first()).toBeVisible()

    // NumCell values — use .first() since the same number could appear in sidebar too.
    await expect(page.getByText('12').first()).toBeVisible()
    await expect(page.getByText('7').first()).toBeVisible()
    await expect(page.getByText('48').first()).toBeVisible()
    await expect(page.getByText('21').first()).toBeVisible()
  })

  test('session with is_baseline shows "★ baseline" pill', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'baseline-session', is_baseline: true, trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'compare-session', trace_count: 3, span_count: 6 }),
    ]
    await stubBackend(page, { sessions, activeId: 'sess-2' })
    await page.goto('/sessions')

    await expect(page.getByText('★ baseline', { exact: true })).toBeVisible()
  })

  test('active session shows "● active" pill and no switch/delete buttons for it', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'active-session', trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'other-session', trace_count: 3, span_count: 6 }),
    ]
    // sess-1 is active
    await stubBackend(page, { sessions, activeId: 'sess-1' })
    await page.goto('/sessions')

    await expect(page.getByText('● active')).toBeVisible()

    // Only the non-active session (sess-2) should have switch and × buttons.
    await expect(page.getByRole('button', { name: /^switch$/ })).toHaveCount(1)
    await expect(page.getByRole('button', { name: /^×$/ })).toHaveCount(1)
  })

  test('clicking star on non-baseline session calls POST /api/sessions/:id/baseline', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'to-baseline', trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'active-sess', trace_count: 3, span_count: 6 }),
    ]

    const baselineCalls: string[] = []
    await stubBackend(page, { sessions, activeId: 'sess-2' }, {
      onBaseline: id => baselineCalls.push(id),
    })

    await page.goto('/sessions')
    await expect(page.getByText('to-baseline').first()).toBeVisible()

    // Both rows have star buttons; sess-1 is non-baseline → title="Mark as baseline".
    // Use the first star with that title (sess-1 row renders first).
    await page.getByTitle('Mark as baseline').first().click()

    await expect.poll(() => baselineCalls.length).toBeGreaterThan(0)
    expect(baselineCalls[0]).toBe('sess-1')
  })

  test('clicking "+ compare" on a non-baseline session enables Compare button', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'baseline-sess', is_baseline: true, trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'compare-sess', trace_count: 3, span_count: 6 }),
    ]
    // sess-2 is active (non-baseline) → shows "+ compare"
    await stubBackend(page, { sessions, activeId: 'sess-2' })
    await page.goto('/sessions')

    // Compare button starts disabled
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeDisabled()

    // feature is active + non-baseline → shows "+ compare"
    await page.getByRole('button', { name: /^\+ compare$/ }).click()

    // baseline=sess-1 (is_baseline:true from list), compare=sess-2 → canDiff=true
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeEnabled()
  })

  test('"Compare sessions" navigates to /diff?baseline=X&compare=Y', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'baseline-nav', is_baseline: true, trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'compare-nav', trace_count: 3, span_count: 6 }),
    ]
    await stubBackend(page, { sessions, activeId: 'sess-2' })

    await page.goto('/sessions')
    await expect(page.getByText('baseline-nav').first()).toBeVisible()

    // sess-2 is active + non-baseline → "+ compare"
    await page.getByRole('button', { name: /^\+ compare$/ }).click()
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeEnabled()

    await page.getByRole('button', { name: /compare sessions/i }).click()

    await expect(page).toHaveURL(/\/diff\?baseline=sess-1&compare=sess-2/)
  })

  test('"switch" button calls POST /api/sessions/:id/activate', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'active-now', trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'switch-target', trace_count: 3, span_count: 6 }),
    ]

    const activateCalls: string[] = []
    // sess-1 is active
    await stubBackend(page, { sessions, activeId: 'sess-1' }, {
      onActivate: id => activateCalls.push(id),
    })

    await page.goto('/sessions')
    await expect(page.getByText('switch-target').first()).toBeVisible()

    // "switch" appears only for the non-active session (sess-2)
    await page.getByRole('button', { name: /^switch$/ }).click()

    await expect.poll(() => activateCalls.length).toBeGreaterThan(0)
    expect(activateCalls[0]).toBe('sess-2')
  })

  test('delete button prompts confirm then calls DELETE /api/sessions/:id — session disappears', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'keeper-session', trace_count: 5, span_count: 10 }),
      makeSession({ id: 'sess-2', label: 'delete-me-session', trace_count: 3, span_count: 6 }),
    ]

    const deleteCalls: string[] = []

    // Accept the confirm dialog.
    page.on('dialog', d => d.accept())

    await stubBackend(page, { sessions, activeId: 'sess-1' }, {
      onDelete: id => deleteCalls.push(id),
    })
    await page.goto('/sessions')
    await expect(page.getByText('delete-me-session').first()).toBeVisible()

    // × button only appears on non-active sessions; sess-2 is non-active.
    await page.getByRole('button', { name: /^×$/ }).click()

    await expect.poll(() => deleteCalls.length).toBeGreaterThan(0)
    expect(deleteCalls[0]).toBe('sess-2')

    // After reload, delete-me-session should be gone.
    await expect(page.getByText('delete-me-session')).toHaveCount(0)
  })

  test('CompareBar is always visible and shows placeholders until both sides are picked', async ({ page }) => {
    const sessions = [
      makeSession({ id: 'sess-1', label: 'solo-session', trace_count: 5, span_count: 10 }),
    ]
    await stubBackend(page, { sessions, activeId: 'sess-1' })
    await page.goto('/sessions')

    // Both placeholders visible before any selection
    await expect(page.getByText('— pick baseline —')).toBeVisible()
    await expect(page.getByText('— pick comparison —')).toBeVisible()
    // Compare button is visible but disabled
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /compare sessions/i })).toBeDisabled()
  })

  test('Import dialog opens when "import trace" sidebar button is clicked', async ({ page }) => {
    await stubBackend(page, { sessions: [], activeId: '' })
    await page.goto('/sessions')

    // "import trace" is rendered as role="button" (a div with role="button") in the sidebar.
    await page.getByRole('button', { name: /import trace/i }).click()

    // Modal shows "Import trace" heading — scope to the modal span to avoid sidebar collision.
    await expect(page.locator('span', { hasText: 'Import trace' })).toBeVisible()
    await expect(page.getByText(/Drop OTLP JSON or Jaeger JSON here/i)).toBeVisible()
  })
})
