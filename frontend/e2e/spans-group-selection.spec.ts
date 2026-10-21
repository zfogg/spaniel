import { test, expect } from '@playwright/test'

const span = (id: string, name: string) => ({
  span_id: id,
  trace_id: 'trace-1',
  parent_span_id: '',
  service_name: 'api',
  name,
  kind: 1,
  start_ns: 1700000000000000000,
  end_ns: 1700000000010000000,
  duration_ns: 10000000,
  status_code: 0,
  status_message: '',
  attributes: '{}',
  resource: '{}',
  session_id: 's',
  received_at: 1700000000000000000,
  events: [],
  links: [],
})
const spans = [span('one', 'first.operation'), span('two', 'second.operation')]
const groups = spans.map((s) => ({
  service_name: s.service_name,
  name: s.name,
  kind: s.kind,
  count: 5,
  attribute_variants: 1,
  error_count: 0,
  latest_start_ns: s.start_ns,
  p50_duration_ns: s.duration_ns,
  p95_duration_ns: s.duration_ns,
  max_duration_ns: s.duration_ns,
}))

test.beforeEach(async ({ page }) => {
  await page.routeWebSocket('**/ws', (ws) => ws.close())
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const url = new URL(route.request().url())
      let data: unknown = []
      if (url.pathname === '/api/spans') {
        data =
          url.searchParams.get('view') === 'grouped'
            ? groups
            : url.searchParams.has('name')
              ? spans.filter((s) => s.name === url.searchParams.get('name'))
              : spans
      } else if (/^\/api\/spans\/[^/]+$/.test(url.pathname)) {
        data = spans.find((s) => s.span_id === url.pathname.split('/').pop())
      } else if (url.pathname === '/api/stats') data = { span_count: 10 }
      else if (url.pathname === '/api/sessions/active') data = { id: '', label: '' }
      await route.fulfill({
        json: { data, meta: { total: Array.isArray(data) ? data.length : 1, page: 1 } },
      })
    },
  )
})

test('group selection keeps other groups visible and opens the inspector', async ({ page }) => {
  await page.goto('/spans?view=grouped')
  const first = page.getByTestId('span-group-api-first.operation')
  const second = page.getByTestId('span-group-api-second.operation')
  await first.click()
  await expect(first).toHaveAttribute('aria-pressed', 'true')
  await expect(second).toBeVisible()
  const inspector = page.getByTestId('span-inspector')
  await expect(inspector).toContainText('first.operation')
  await expect(page).toHaveURL(/view=grouped/)
  await second.click()
  await expect(second).toHaveAttribute('aria-pressed', 'true')
  await expect(first).toHaveAttribute('aria-pressed', 'false')
  await expect(inspector).toContainText('second.operation')
  await inspector.getByRole('button', { name: 'Close inspector' }).click()
  await expect(inspector).toHaveCount(0)
  await expect(first).toBeVisible()
  await expect(second).toBeVisible()
  expect(new URL(page.url()).searchParams.has('groupName')).toBe(false)
})

test('legacy group URLs never filter the main span list', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/spans' && url.searchParams.get('limit') === '100')
      requests.push(url.search)
  })
  await page.goto('/spans?view=all&groupService=api&groupName=first.operation&groupKind=1&span=one')
  await expect(page.getByTestId('span-row-one')).toBeVisible()
  await expect(page.getByTestId('span-row-two')).toBeVisible()
  await expect(page.getByTestId('span-inspector')).toContainText('first.operation')
  expect(requests.length).toBeGreaterThan(0)
  expect(requests.every((search) => !new URLSearchParams(search).has('name'))).toBe(true)
})
