import { test, expect, type Page } from '@playwright/test'

test('probe', async ({ page }) => {
  page.on('request', r => {
    if (r.url().includes('/api/')) console.log('REQ', r.url())
  })
  page.on('response', r => {
    if (r.url().includes('/api/')) console.log('RES', r.status(), r.url())
  })

  await page.routeWebSocket('**/ws', ws => ws.close())
  await page.route('**/api/stats*', r => r.fulfill({ status: 200, contentType: 'application/json', body: '{"data":{},"meta":{}}' }))
  await page.route('**/api/forwarders', r => r.fulfill({ status: 200, contentType: 'application/json', body: '{"data":[],"meta":{}}' }))
  await page.route('**/api/sessions/active', r => r.fulfill({ status: 200, contentType: 'application/json', body: '{"data":{},"meta":{}}' }))
  await page.route(/\/api\/metrics(\/series)?(\?|$)/, r => {
    const url = new URL(r.request().url())
    console.log('MATCHED METRICS ROUTE', url.pathname, url.search)
    const body = url.pathname.endsWith('/series')
      ? { data: { name: '', service_name: '', type: 'gauge', unit: '', description: '', points: [] } }
      : { data: [
          { name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: '', sample_count: 1 },
          { name: 'pool.in_use', service_name: 'postgres', type: 'gauge', unit: 'conn', description: '', sample_count: 1 },
        ] }
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...body, meta: {} }) })
  })

  await page.goto('/metrics')
  await page.waitForTimeout(2000)
  const text = await page.locator('body').innerText()
  console.log('---BODY---')
  console.log(text.slice(0, 500))
})
