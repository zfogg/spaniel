import { expect, test } from '@playwright/test'

test('bar, pie and gauge panels render saved query results and survive reload', async ({
  page,
}) => {
  const requests: string[] = []
  const panels = ['single_value', 'bar', 'pie', 'gauge', 'time_series'].map((kind, position) => ({
    id: kind,
    dashboard_id: 'charts',
    title: `${kind} requests`,
    display_type: kind,
    query_sql: "SELECT 'orders' AS label, 25 AS value",
    query_version: 1,
    settings_json: kind === 'gauge' ? '{"min":0,"max":200,"unit":"ms"}' : '{}',
    layout_json: JSON.stringify({ x: 1, y: position + 1, w: 12, h: 1 }),
    position,
  }))
  const dashboard = {
    id: 'charts',
    name: 'Chart coverage',
    description: '',
    variables: [],
    panels,
    created_at: 1,
    updated_at: 1,
  }
  await page.routeWebSocket('**/ws', () => {})
  await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/dashboards') data = [dashboard]
    else if (path === '/api/dashboards/charts') data = dashboard
    else if (path.endsWith('/query-preview')) {
      const kind = route.request().postDataJSON().display_type as string
      requests.push(kind)
      data = {
        columns: ['label', 'value'],
        rows:
          kind === 'gauge'
            ? [{ value: 50 }]
            : kind === 'time_series'
              ? Array.from({ length: 20 }, (_, i) => ({
                  timestamp_ns: 1000000000,
                  group_value: `ascii-chat-mirror / ascii-chat.pipeline-long-series-${i}`,
                  value: i,
                }))
              : [
                  { label: 'orders', value: 25 },
                  ...(kind === 'bar'
                    ? [
                        {
                          label:
                            'ascii-chat-client / ascii-chat.audio.buffer.overruns-with-a-long-label',
                          value: 99,
                        },
                      ]
                    : []),
                  { label: 'payments', value: 75 },
                  ...(kind === 'bar'
                    ? Array.from({ length: 30 }, (_, i) => ({ label: `category-${i}`, value: i }))
                    : []),
                ],
        warnings: [],
      }
    } else if (path === '/api/sessions/active') data = { id: 'session', label: 'Session' }
    else if (path === '/api/stats')
      data = { span_count: 0, trace_count: 0, log_count: 0, db_size: 0, session_count: 1 }
    await route.fulfill({ json: { data } })
  })
  await page.goto('/dashboards')
  for (let attempt = 0; attempt < 2; attempt++) {
    for (const kind of ['bar', 'pie', 'gauge'])
      await expect(page.getByTestId(`${kind}-panel`)).toBeVisible()
    await expect(page.getByRole('img', { name: 'orders: 25', exact: true })).toBeVisible()
    await expect(page.getByTestId('pie-panel')).toContainText('25 (25%)')
    await expect(page.getByTestId('gauge-panel').getByRole('img')).toHaveAttribute(
      'aria-label',
      'gauge requests: 50 ms; range 0 to 200',
    )
    if (attempt === 0) await page.reload()
  }
  for (const kind of ['bar', 'pie', 'gauge']) expect(requests).toContain(kind)
  const single = page.getByRole('heading', { name: 'single_value requests', exact: true })
  expect(
    await single.evaluate((el) => el.closest('article')!.getBoundingClientRect().height),
  ).toBeLessThan(240)
  const bars = page.getByTestId('bar-panel')
  const longLabel = bars.getByRole('button', {
    name: 'ascii-chat-client / ascii-chat.audio.buffer.overruns-with-a-long-label',
    exact: true,
  })
  await longLabel.focus()
  await expect(
    page.getByRole('tooltip').filter({
      hasText: 'ascii-chat-client / ascii-chat.audio.buffer.overruns-with-a-long-label',
    }),
  ).toBeVisible()
  expect(await longLabel.evaluate((el) => getComputedStyle(el).textOverflow)).toBe('ellipsis')
  await page.mouse.move(0, 0)
  expect(await bars.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  await bars.hover()
  await page.mouse.wheel(0, 500)
  await expect.poll(() => bars.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
  await bars.focus()
  await page.keyboard.press('End')
  await expect(bars.getByText('category-29', { exact: true })).toBeInViewport()
  const legend = page.getByRole('region', { name: 'time_series requests legend', exact: true })
  expect(await legend.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  const seriesLabel = legend.getByRole('button').first()
  await seriesLabel.focus()
  await expect(
    page
      .getByRole('tooltip')
      .filter({ hasText: 'ascii-chat-mirror / ascii-chat.pipeline-long-series-0' }),
  ).toBeVisible()
  expect(await seriesLabel.evaluate((el) => getComputedStyle(el).textOverflow)).toBe('ellipsis')
  expect(await seriesLabel.evaluate((el) => getComputedStyle(el).fontSize)).toBe('12px')
  await legend.focus()
  await page.keyboard.press('End')
  await expect.poll(() => legend.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
  await page.setViewportSize({ width: 390, height: 844 })
  for (const kind of ['bar', 'pie', 'gauge']) {
    const panel = page.getByTestId(`${kind}-panel`)
    await panel.scrollIntoViewIfNeeded()
    await expect(panel).toBeVisible()
    expect(
      await panel.evaluate((element) => element.scrollWidth <= element.clientWidth),
      kind,
    ).toBe(true)
  }
  await expect
    .poll(() =>
      page.getByRole('heading', { name: 'time_series requests', exact: true }).evaluate((el) => {
        const text = el.closest('article')!.querySelector('svg text') as SVGTextElement
        return parseFloat(getComputedStyle(text).fontSize) * text.getScreenCTM()!.a
      }),
    )
    .toBeGreaterThan(11)
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.getByRole('button', { name: 'Dashboard settings', exact: true }).click()
  const live = page.getByRole('switch', { name: 'Live rolling time window', exact: true })
  await live.check()
  await page.getByRole('button', { name: 'Done', exact: true }).click()
  await page.reload()
  await page.getByRole('button', { name: 'Dashboard settings', exact: true }).click()
  await expect(live).toBeChecked()
  await live.uncheck()
  await page.getByRole('button', { name: 'Done', exact: true }).click()
  await page.goto('/dashboards/charts')
  for (const [kind, label] of [
    ['bar', 'Bar Compare categories'],
    ['pie', 'Pie Share of a total'],
    ['gauge', 'Gauge Value within a range'],
  ]) {
    await page.getByRole('button', { name: new RegExp(label) }).click()
    await page.getByRole('button', { name: 'Use generated SQL', exact: true }).click()
    if (kind === 'gauge') {
      await page.getByLabel('Minimum', { exact: true }).fill('0')
      await page.getByLabel('Maximum', { exact: true }).fill('200')
      await page.getByLabel('Unit', { exact: true }).fill('ms')
    }
    await page.getByRole('button', { name: 'Run preview', exact: true }).click()
    const preview = page.getByRole('region', { name: 'Panel preview', exact: true })
    await expect(preview.getByTestId(`${kind}-panel`)).toBeVisible()
    if (kind === 'gauge')
      await expect(preview.getByRole('img')).toHaveAttribute('aria-label', /range 0 to 200/)
  }
})
