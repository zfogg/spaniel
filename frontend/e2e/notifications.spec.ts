import { expect, test } from '@playwright/test'

test('grouped navigation, nested docs, paginated notification history and live updates', async ({
  page,
}) => {
  let live = false
  const requests: string[] = []
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const url = new URL(route.request().url())
    let data: unknown = []
    let total = 0
    if (url.pathname === '/api/notifications') {
      requests.push(url.search)
      expect(url.searchParams.get('limit')).toBe('35')
      const second = url.searchParams.get('page') === '2'
      total = live ? 37 : 36
      data = Array.from({ length: second ? 1 : 35 }, (_, i) => ({
        id: `${second ? 35 : i}`,
        source: 'alert',
        severity: 'warning',
        title:
          live && i === 0 && !second ? 'Live browser alert' : `Notification ${second ? 36 : i + 1}`,
        body: 'Browser toast history',
        created_at: Date.now() * 1e6,
      }))
    } else if (url.pathname === '/api/stats') data = {}
    else if (url.pathname === '/api/sessions/active') data = { id: 'session', label: 'Test' }
    await route.fulfill({
      json: { data, meta: { total, page: Number(url.searchParams.get('page') ?? 1) } },
    })
  })
  let send: (data: string) => void = () => {}
  await page.routeWebSocket('**/ws', (socket) => {
    send = (data) => socket.send(data)
  })
  await page.goto('/notifications')
  await expect(page.getByRole('article')).toHaveCount(35)
  await expect(page.getByText('Live updates connected')).toBeVisible()
  const nav = page.getByRole('navigation', { name: 'Primary navigation' })
  await expect(nav.getByRole('link')).toHaveText([
    'Services',
    'Sessions',
    'Notifications',
    'Settings',
  ])
  await nav.getByRole('button', { name: 'Telemetry' }).click()
  await expect(page.getByRole('menuitem')).toHaveText(['Traces', 'Spans', 'Logs', 'Metrics'])
  await page.keyboard.press('Escape')
  await nav.getByRole('button', { name: 'Monitor' }).click()
  await expect(page.getByRole('menuitem')).toHaveText(['Dashboards', 'Alerts'])
  await page.keyboard.press('Escape')
  await nav.getByRole('button', { name: 'Tools' }).click()
  await page.getByRole('menuitem', { name: 'Docs', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('menuitem', { name: 'Database schema' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Next notifications page' }).click()
  await expect(page).toHaveURL(/page=2/)
  await expect(page.getByRole('article')).toHaveCount(1)
  await expect(page.getByText('Notification 36', { exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByText('Notification 36', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Previous notifications page' }).click()
  live = true
  send(
    JSON.stringify({
      type: 'notification',
      timestamp_ns: Date.now() * 1e6,
      payload: { id: 'live', source: 'alert', sourceId: 'rule' },
    }),
  )
  await expect(page.getByText('Live browser alert', { exact: true })).toBeVisible()
  expect(requests.length).toBeGreaterThan(3)
  await page.setViewportSize({ width: 1100, height: 800 })
  await expect(nav.getByRole('link', { name: 'Settings' })).toBeVisible()
})
