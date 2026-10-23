import { test, expect } from '@playwright/test'

// Run against an isolated collector with a fresh database and config home:
// SPANIEL_E2E_BASE_URL=http://127.0.0.1:18084 SPANIEL_E2E_LIVE_SELF_MONITOR=1
// pnpm exec playwright test e2e/self-monitor-live.spec.ts

test('self-monitoring buttons and switch install real bundled definitions', async ({
  page,
  request,
}) => {
  test.skip(
    process.env.SPANIEL_E2E_LIVE_SELF_MONITOR !== '1',
    'Requires an isolated live collector',
  )
  await page.goto('/settings')
  const dashboardsButton = page.getByRole('button', { name: 'Create / refresh Spaniel dashboards' })
  await dashboardsButton.click()
  await expect(page.getByRole('status')).toContainText('3 created')
  await dashboardsButton.click()
  await expect(page.getByRole('status')).toContainText('3 refreshed')
  await page.getByRole('button', { name: 'Create Spaniel alerts', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('3 created')
  await page.getByRole('button', { name: 'Create Spaniel alerts', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('3 existing alerts kept')

  const toggle = page.getByRole('switch', { name: 'self monitor', exact: true })
  page.once('dialog', (dialog) => dialog.accept())
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByRole('status')).toContainText('3 refreshed')
  await expect(page.getByRole('status')).toContainText('3 existing alerts kept')
  await expect
    .poll(async () => (await (await request.get('/api/settings')).json()).data.self_monitor)
    .toBe(true)
  await toggle.click()
  await expect
    .poll(async () => (await (await request.get('/api/settings')).json()).data.self_monitor)
    .toBe(false)
  page.once('dialog', (dialog) => dialog.dismiss())
  await toggle.click()
  await expect
    .poll(async () => (await (await request.get('/api/settings')).json()).data.self_monitor)
    .toBe(true)
  const dashboards = (await (await request.get('/api/dashboards')).json()).data
  const alerts = (await (await request.get('/api/alerts')).json()).data.items
  expect(dashboards).toHaveLength(3)
  expect(alerts).toHaveLength(3)
  await toggle.click()
  await expect
    .poll(async () => (await (await request.get('/api/settings')).json()).data.self_monitor)
    .toBe(false)
})
