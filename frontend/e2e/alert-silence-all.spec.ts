import { expect, test, type Page } from '@playwright/test'
import { emptyRule } from '../src/pages/alerts/alert-model'

async function setup(page: Page, fail = false) {
  const rule = { ...emptyRule(), id: 'mute-rule', name: 'Error traces' }
  const requests: Record<string, unknown>[] = []
  const silences: Record<string, unknown>[] = []
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/alerts/mute-rule/silences' && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      requests.push(body)
      if (fail) {
        await route.fulfill({ status: 500, json: { error: 'Could not save silence' } })
        return
      }
      const silence = { ...body, id: 'silence-one', rule_id: rule.id, starts_at: Date.now() * 1e6 }
      silences.push(silence)
      data = silence
    } else if (path === '/api/alerts')
      data = { items: [rule], summary: { rule_counts: {}, instance_counts: {} } }
    else if (path === '/api/alerts/mute-rule') data = rule
    else if (path === '/api/sessions/active') data = { id: 'session', label: 'Session' }
    else if (path === '/api/stats')
      data = { total_traces: 0, total_spans: 0, total_logs: 0, total_metrics: 0 }
    await route.fulfill({ json: { data, silences, meta: { total: 1, page: 1, limit: 15 } } })
  })
  await page.goto('/alerts?state=all&id=mute-rule')
  return requests
}

test('silences the whole rule for a chosen duration and refreshes its silence list', async ({
  page,
}) => {
  const requests = await setup(page)
  await page.getByRole('button', { name: 'Silence all', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Silence all instances' })
  await expect(dialog.getByLabel('Duration', { exact: true })).toHaveValue('1h')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  expect(requests).toHaveLength(0)
  await page.getByRole('button', { name: 'Silence all', exact: true }).click()
  await expect(dialog).toContainText('all current and new instances')
  await dialog.getByLabel('Duration', { exact: true }).fill('5m')
  await dialog.getByLabel('Reason (optional)', { exact: true }).fill('Planned maintenance')
  const before = Date.now() * 1e6
  await dialog.getByRole('button', { name: 'Silence all', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(requests).toHaveLength(1)
  expect(requests[0].group_key).toBe('')
  expect(requests[0].comment).toBe('Planned maintenance')
  expect(requests[0].ends_at).toBeGreaterThanOrEqual(before + 300e9)
  expect(requests[0].ends_at).toBeLessThanOrEqual(Date.now() * 1e6 + 300e9)
  await expect(page.getByText('Planned maintenance', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Revoke', exact: true })).toBeVisible()
})

test('rejects zero duration and keeps a failed silence open for retry', async ({ page }) => {
  const requests = await setup(page, true)
  await page.getByRole('button', { name: 'Silence all', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Silence all instances' })
  await dialog.getByLabel('Duration', { exact: true }).fill('0s')
  await dialog.getByRole('button', { name: 'Silence all', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('greater than zero')
  expect(requests).toHaveLength(0)
  await dialog.getByLabel('Duration', { exact: true }).fill('1h')
  await dialog.getByRole('button', { name: 'Silence all', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('Could not save silence')
  await expect(dialog.getByRole('button', { name: 'Silence all', exact: true })).toBeEnabled()
})
