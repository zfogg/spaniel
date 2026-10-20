import { test, expect } from '@playwright/test'

for (const signal of ['traces', 'logs'] as const) {
  test(`${signal} retain content and controls during service filter requests`, async ({ page }) => {
    await page.routeWebSocket('**/ws', (ws) => ws.close())
    let release: (() => void) | undefined
    let pending = false
    await page.route(
      (url) => url.pathname.startsWith('/api/'),
      async (route) => {
        const url = new URL(route.request().url())
        let data: unknown = []
        if (url.pathname === '/api/services') data = ['api', 'worker', 'empty']
        if (url.pathname === '/api/sessions/active') data = { id: '', label: '' }
        if (url.pathname === `/api/${signal}`) {
          if (pending)
            await new Promise<void>((resolve) => {
              release = resolve
            })
          const services = url.searchParams.getAll('services')
          data = ['api', 'worker']
            .filter((s) => !services.length || services.includes(s))
            .map((service, index) => ({
              trace_id: service,
              span_id: service,
              service_name: service,
              session_id: 's1',
              name: `${service} operation`,
              body: `${service} operation`,
              severity: 9,
              attributes: '{}',
              start_ns: Date.now() * 1_000_000,
              timestamp_ns: (Date.now() + index) * 1_000_000,
              duration_ns: 10_000_000,
              status_code: 1,
              span_count: 1,
            }))
        }
        await route.fulfill({
          json: { data, meta: { total: Array.isArray(data) ? data.length : 0, page: 1 } },
        })
      },
    )
    await page.goto(`${signal === 'traces' ? '/' : '/logs'}?services=api&services=worker`)
    await expect(page.getByText('api operation', { exact: true })).toBeVisible()
    if (signal === 'logs') await page.getByText('2 services selected', { exact: true }).click()

    // Hold each new response so transient loading/empty states cannot hide
    // behind a fast localhost request. Also cover a disjoint service selection.
    for (const [service, visible] of [
      ['worker', 'api'],
      ['api', 'api'],
      ['empty', 'api'],
    ] as const) {
      pending = true
      release = undefined
      await page.getByRole('checkbox', { name: service, exact: true }).click()
      await expect.poll(() => !!release).toBe(true)
      await expect(page.getByText(`${visible} operation`, { exact: true })).toBeVisible()
      await expect(page.getByRole('checkbox', { name: service, exact: true })).toBeVisible()
      await expect(page.getByText(/^(Loading…|loading…)$/)).toHaveCount(0)
      pending = false
      release!()
      if (service === 'empty') {
        await expect(
          page.getByText(
            signal === 'traces' ? 'No traces match the current filters.' : 'No logs yet',
          ),
        ).toBeVisible()
      } else if (service === 'worker') {
        await expect(page.getByText('worker operation', { exact: true })).toHaveCount(0)
      } else {
        await expect(page.getByText('worker operation', { exact: true })).toBeVisible()
      }
    }
  })
}
