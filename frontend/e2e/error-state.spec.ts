import { test, expect } from '@playwright/test'

for (const recursive of [true, false]) {
  test(`trace error stays readable and scrollable (recursive=${recursive})`, async ({ page }) => {
    await page.setViewportSize({ width: 560, height: 460 })
    await page.routeWebSocket('**/ws', (ws) => ws.close())
    const error = recursive
      ? 'Failed: database has been invalidated because of a previous fatal error. Original error: "'.repeat(
          10000,
        ) +
        'Could not find node in column segment tree' +
        '"'.repeat(10000)
      : 'long error line\n'.repeat(10000)
    await page.route('**/api/**', (route) =>
      route.fulfill({
        status: route.request().url().includes('/api/traces/abc123') ? 500 : 200,
        contentType: 'application/json',
        body: JSON.stringify(
          route.request().url().includes('/api/traces/abc123') ? { error } : { data: [] },
        ),
      }),
    )
    await page.goto('/traces/abc123?view=waterfall')
    const panel = page.getByTestId('error-state')
    await expect(panel).toBeVisible()
    expect((await panel.textContent())!.length).toBeLessThan(4500)
    await expect(panel.getByText("Couldn't load this trace")).toBeVisible()
    if (recursive)
      await expect(panel.getByText(/Could not find node in column segment tree/)).toBeVisible()
    const retry = panel.getByRole('button', { name: 'Retry' })
    await retry.scrollIntoViewIfNeeded()
    await expect(retry).toBeInViewport()
    if (!recursive) expect(await panel.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
    await page.screenshot({ path: `test-results/error-state-${recursive}.png` })
  })
}
