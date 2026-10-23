import { expect, test, type Page } from '@playwright/test'
import { emptyRule } from '../src/pages/alerts/alert-model'

async function setup(page: Page, fail = false) {
  const rule = {
    ...emptyRule(),
    id: 'bulk-alert',
    name: 'Many error traces',
    instances: Array.from({ length: 32 }, (_, index) => ({
      rule_id: 'bulk-alert',
      group_key: `trace=${index}`,
      state: index === 31 ? 'pending' : 'firing',
      last_evaluated_at: Date.now() * 1e6,
      acknowledged_at: null as number | null,
    })),
  }
  let calls = 0
  let release = () => {}
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/alerts/bulk-alert/acknowledge') {
      calls++
      await pending
      if (fail) {
        await route.fulfill({ status: 500, json: { error: 'Acknowledgement failed' } })
        return
      }
      for (const instance of rule.instances) instance.acknowledged_at = Date.now() * 1e6
      data = { ok: true }
    } else if (path === '/api/alerts') {
      data = { items: [rule], summary: { rule_counts: {}, instance_counts: {} } }
    } else if (path === '/api/alerts/bulk-alert') data = rule
    else if (path === '/api/sessions/active') data = { id: 'session', label: 'Session' }
    else if (path === '/api/stats') data = {}
    await route.fulfill({ json: { data, meta: { total: 1, page: 1, limit: 15 } } })
  })
  await page.goto('/alerts?state=all&id=bulk-alert')
  await expect(page.getByRole('heading', { name: rule.name })).toBeVisible()
  return { rule, release, calls: () => calls }
}

test('acknowledges every instance across pages and refreshes immediately', async ({ page }) => {
  const fixture = await setup(page)
  const section = page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Instances', exact: true }) })
  const button = section.getByRole('button', { name: 'Acknowledge all', exact: true })
  await expect(button).toHaveAttribute('title', /all 32 pending or firing instances/)
  await button.click()
  await expect(section.getByRole('button', { name: 'Acknowledging…' })).toBeDisabled()
  fixture.release()
  await expect(button).toBeDisabled()
  expect(fixture.calls()).toBe(1)
  expect(fixture.rule.instances.every((instance) => instance.acknowledged_at)).toBe(true)
  await expect(section.getByRole('button', { name: 'Unacknowledge', exact: true })).toHaveCount(15)
  await page.getByRole('button', { name: 'Next instances page' }).click()
  await page.getByRole('button', { name: 'Next instances page' }).click()
  await expect(section.getByText('acknowledged pending', { exact: true })).toBeVisible()
  await expect(section.getByRole('button', { name: 'Unacknowledge', exact: true })).toHaveCount(2)
})

test('keeps acknowledgement available and shows a failed request', async ({ page }) => {
  const fixture = await setup(page, true)
  const button = page.getByRole('button', { name: 'Acknowledge all', exact: true })
  await button.click()
  fixture.release()
  await expect(page.getByRole('alert')).toContainText('Acknowledgement failed')
  await expect(button).toBeEnabled()
  expect(fixture.rule.instances.every((instance) => instance.acknowledged_at === null)).toBe(true)
})
