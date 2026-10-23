import { expect, test } from '@playwright/test'
import { emptyRule } from '../src/pages/alerts/alert-model'

const rule = { ...emptyRule(), id: 'test-alert', name: 'High latency' }
const dashboard = {
  id: 'test-dashboard',
  name: 'Test dashboard',
  description: '',
  created_at: 1,
  updated_at: 1,
  panels: [],
  variables: [],
}

test.beforeEach(async ({ page }) => {
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/alerts')
      data = { items: [rule], summary: { rule_counts: {}, instance_counts: {} } }
    else if (path === '/api/alerts/test-alert') data = rule
    else if (path === '/api/dashboards') data = [dashboard]
    else if (path === '/api/dashboards/test-dashboard') data = dashboard
    else if (path === '/api/sql/format') data = { sql: route.request().postDataJSON().sql }
    else if (path.endsWith('/config')) {
      await route.fulfill({
        contentType: 'text/yaml',
        body: 'version: 1\nname: Test configuration\n',
      })
      return
    } else if (path === '/api/sessions/active') data = { id: 'test-session', label: 'Test session' }
    else if (path === '/api/stats')
      data = { total_traces: 0, total_spans: 0, total_logs: 0, total_metrics: 0 }
    await route.fulfill({
      json: {
        data,
        meta: { total: Array.isArray(data) ? data.length : 1, page: 1, limit: 15 },
        silences: [],
      },
    })
  })
})

test('alert modules render the inspector, editor, history, import and YAML dialog', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/alerts')
  await expect(page.getByRole('heading', { name: 'High latency' })).toBeVisible()
  await expect(page.getByText('Evaluation timeline', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'View YAML', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Alert YAML configuration' })).toContainText(
    'Test configuration',
  )
  await page.getByRole('button', { name: 'Close', exact: true }).click()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByLabel('Alert title', { exact: true })).toHaveValue('High latency')
  await page.getByRole('button', { name: 'Cancel', exact: true }).first().click()
  await page.getByRole('button', { name: 'History', exact: true }).click()
  await expect(page.getByLabel('History alert rule')).toBeVisible()
  await page.getByRole('button', { name: 'New rule', exact: true }).click()
  await expect(page.getByLabel('Alert title', { exact: true })).toHaveValue('New alert')
  await page.getByRole('button', { name: 'Cancel', exact: true }).first().click()
  await page.getByRole('button', { name: 'Import YAML', exact: true }).first().click()
  await expect(
    page.getByText('Validation happens before the definition is saved.', { exact: false }),
  ).toBeVisible()
  expect(errors).toEqual([])
})

test('dashboard gallery, editor and creation routes retain their distinct behavior', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/dashboards')
  await expect(page.getByRole('heading', { name: 'Test dashboard' })).toBeVisible()
  await page.goto('/dashboards/test-dashboard')
  await expect(page.getByLabel('Dashboard name', { exact: true })).toHaveValue('Test dashboard')
  await expect(page.getByRole('button', { name: 'Run preview', exact: true })).toBeVisible()
  await page.goto('/dashboards/new')
  await expect(page.getByRole('heading', { name: 'Choose a starting point' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Delete dashboard' })).toHaveCount(0)
  expect(errors).toEqual([])
})

test('dashboard and alert SQL editors format only on request and retain comments and parameters', async ({
  page,
}) => {
  const raw = '-- keep\nselect id,name from spans where trace_id=$trace_id; -- trailing'
  const formatted =
    '-- keep\nselect\n  id,\n  name\nfrom spans\nwhere trace_id=$trace_id; -- trailing'
  let calls = 0
  await page.route('**/api/sql/format', async (route) => {
    const body = route.request().postDataJSON()
    if (body.sql === raw) calls++
    await route.fulfill({ json: { data: { sql: body.sql === raw ? formatted : body.sql } } })
  })
  for (const kind of ['dashboard', 'alert']) {
    if (kind === 'dashboard') await page.goto('/dashboards/test-dashboard')
    else {
      await page.goto('/alerts')
      await page.getByRole('button', { name: 'Edit', exact: true }).click()
    }
    const label = kind === 'dashboard' ? 'DuckDB SQL' : 'Alert SQL query'
    const editor = page.getByLabel(label, { exact: true }).locator('.cm-content')
    const before = calls
    await editor.fill(raw)
    await expect
      .poll(async () => (await editor.locator('.cm-line').allTextContents()).join('\n'))
      .toBe(raw)
    expect(calls).toBe(before)
    await page.getByRole('button', { name: 'Format SQL', exact: true }).first().click()
    await expect
      .poll(async () => (await editor.locator('.cm-line').allTextContents()).join('\n'))
      .toBe(formatted)
    expect(calls).toBe(before + 1)
  }
})
