import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

test.use({ viewport: { width: 1440, height: 1100 } })

// Generated and checked by Go's TestDisplayPlaywrightContractFixture. These
// responses come from Sequel's actual sanitization/layout/source-map pipeline.
function fixture(name: string) {
  return JSON.parse(
    readFileSync(new URL('./fixtures/query-analysis.json', import.meta.url), 'utf8'),
  )[name]
}

async function setup(page: Page, name = 'mapped') {
  const f = fixture(name)
  const attrs = {
    'db.system.name': 'postgresql',
    ...(f.input.sql
      ? { 'db.query.text': f.input.sql }
      : { 'gotel.db.query.text_reason': 'malformed' }),
    'db.query.diagnostics': f.input.diagnostics,
    'gotel.db.query.source_map': f.input.source_map,
    'gotel.db.query.original_length': f.input.original_length,
    'gotel.db.query.diagnostic_schema_version': f.input.diagnostic_schema_version,
  }
  const span = (id: string) => ({
    trace_id: '00000000000000000000000000000001',
    span_id: id,
    parent_span_id: '',
    service_name: 'query-fixture',
    name: 'storage.postgres.FindThings',
    kind: 3,
    start_ns: 1_700_000_000_000_000_000,
    end_ns: 1_700_000_000_001_000_000,
    duration_ns: 1_000_000,
    status_code: 0,
    status_message: '',
    attributes: JSON.stringify(attrs),
    resource: '{}',
    session_id: 'session-fixture',
    session_label: 'SQL mapping fixture',
    received_at: 0,
    events: [],
    links: [],
    sampled: true,
    query_occurrences: 57,
  })
  await page.routeWebSocket('**/ws', (ws) => ws.close())
  await page.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const url = new URL(route.request().url())
      const path = url.pathname
      let data: unknown = []
      let meta = { total: 0, page: 1, limit: 100 }
      if (path === '/api/sql/analyze') {
        const input = route.request().postDataJSON()
        expect(input.sql).toBe(f.input.sql)
        expect(input.source_map).toBe(f.input.source_map)
        data = f.output
      } else if (path === '/api/spans') {
        const pageNo = Number(url.searchParams.get('page') ?? 1)
        data = [span(`span-${pageNo}`)]
        meta = { total: 201, page: pageNo, limit: 100 }
      } else if (path.startsWith('/api/spans/')) data = span(path.split('/').at(-1)!)
      else if (path === '/api/services') data = ['query-fixture']
      else if (path === '/api/sessions/active')
        data = { id: 'session-fixture', label: 'SQL mapping fixture' }
      else if (path === '/api/stats')
        data = { span_count: 201, trace_count: 10, log_count: 0, db_size: 0, session_count: 1 }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data, meta }),
      })
    },
  )
  await page.goto('/spans', { waitUntil: 'domcontentloaded' })
  await page.getByTestId('span-row-span-1').click()
  return f
}

test('underlines exact transformed Unicode/multiline ranges and shows session counts', async ({
  page,
}, info) => {
  const f = await setup(page)
  const inspector = page.getByTestId('span-inspector')
  const query = inspector.getByLabel('SQL query', { exact: true })
  await expect(query).toHaveText(f.output.sql)
  expect(await query.textContent()).toContain('😀é')
  expect((await query.textContent())!.includes('\n')).toBeTruthy()
  const marks = query.getByTestId('sql-diagnostic-range')
  await expect(marks).toHaveCount(2)
  for (let i = 0; i < 2; i++) {
    await expect(marks.nth(i)).toHaveText(')')
    await expect(marks.nth(i)).toHaveAttribute('data-mapping-status', 'exact')
    const offset = await marks.nth(i).evaluate((element) => {
      const root = element.closest('[aria-label="SQL query"]')!
      const before = document.createRange()
      before.setStart(root, 0)
      before.setEndBefore(element)
      return before.toString().length
    })
    expect(offset).toBe(f.output.diagnostics[i].display_start)
  }
  await expect(inspector.getByTestId('sql-diagnostic')).toHaveCount(2)
  await expect(inspector.getByTestId('session-query-occurrences')).toContainText('57')
  await expect(inspector.getByTestId('session-query-occurrences')).toContainText(
    'all pages and display filters',
  )
  await page.screenshot({
    path: info.outputPath('unicode-diagnostics-session-count.png'),
    fullPage: true,
  })
})

test('labels replaced source ranges approximate and unmapped ranges unavailable', async ({
  page,
}, info) => {
  await setup(page, 'approximate')
  const mark = page.getByTestId('span-inspector').getByTestId('sql-diagnostic-range')
  await expect(mark).toHaveText('?')
  await expect(mark).toHaveAttribute('data-mapping-status', 'approximate')
  await expect(
    page.getByText('Approximate location: source text was replaced or removed.'),
  ).toBeVisible()
  await page.screenshot({ path: info.outputPath('approximate-diagnostic.png'), fullPage: true })
})

test('keeps original offsets explanatory when safe provenance is absent', async ({ page }) => {
  await setup(page, 'unavailable')
  await expect(page.getByTestId('span-inspector').getByTestId('sql-diagnostic')).toHaveCount(2)
  await expect(page.getByTestId('sql-diagnostic-range')).toHaveCount(0)
  await expect(
    page.getByText('Underline unavailable: no safe source mapping for the displayed SQL.'),
  ).toHaveCount(2)
})

test('explains omitted malformed text without rendering a raw fallback', async ({ page }) => {
  await setup(page, 'omitted')
  await expect(page.getByTestId('sql-text-reason')).toHaveText(
    'SQL text omitted because the input was malformed.',
  )
  await expect(page.getByTestId('span-inspector').getByTestId('sql-diagnostic')).toHaveCount(1)
  await expect(page.getByLabel('SQL query', { exact: true })).toHaveCount(0)
  await expect(page.getByTestId('sql-diagnostic-range')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('private-fixture-literal')
})

test('session count survives display filtering and pagination', async ({ page }) => {
  await setup(page)
  await expect(page.getByTestId('session-query-occurrences')).toContainText('57')
  await page.getByTestId('spans-search').fill('FindThings')
  await expect(page.getByTestId('span-row-span-1')).toContainText('57 in session')
  await page.getByTestId('spans-search').fill('')
  await page.getByRole('button', { name: /next spans page/i }).click()
  await page.getByTestId('span-row-span-2').click()
  await expect(page.getByTestId('session-query-occurrences')).toContainText('57')
  await expect(page.getByTestId('session-query-occurrences')).toContainText('this session')
})

test('editor underlines its exact user-owned Unicode document and removes stale decorations', async ({
  page,
}) => {
  await setup(page)
  const f = fixture('editor')
  await expect(page.getByLabel('SQL query', { exact: true })).toHaveText(
    fixture('mapped').output.sql,
  )
  const dashboard = {
    id: 'test-map',
    name: 'Mapping fixture',
    description: '',
    created_at: 1,
    updated_at: 1,
    panels: [],
    variables: [],
  }
  await page.route(
    (url) => url.pathname === '/api/dashboards' || url.pathname === '/api/dashboards/test-map',
    (route) =>
      route.fulfill({
        json: {
          data:
            new URL(route.request().url()).pathname === '/api/dashboards' ? [dashboard] : dashboard,
        },
      }),
  )
  await page.route('**/api/sql/analyze', async (route) => {
    const input = route.request().postDataJSON()
    expect(input.source).toBe('original')
    if (input.sql !== f.input.sql) await new Promise((resolve) => setTimeout(resolve, 300))
    await route.fulfill({
      json: {
        data:
          input.sql === f.input.sql
            ? f.output
            : { sql: input.sql, status: 'ok', reason: '', diagnostics: [] },
      },
    })
  })
  await page.goto('/dashboards/test-map')
  const editor = page.getByLabel('DuckDB SQL', { exact: true }).locator('.cm-content').first()
  await editor.click()
  await editor.press('ControlOrMeta+A')
  await page.keyboard.insertText(f.input.sql)
  await expect
    .poll(async () => (await editor.locator('.cm-line').allTextContents()).join('\n'))
    .toBe(f.input.sql.replaceAll('\r\n', '\n'))
  const expected = f.output.sql.slice(
    f.output.diagnostics[0].display_start,
    f.output.diagnostics[0].display_end,
  )
  await expect
    .poll(async () => (await editor.getByTestId('sql-diagnostic-range').allTextContents()).join(''))
    .toBe(expected)
  // A changed document has no right to reuse the previous response's ranges.
  await editor.press('End')
  await page.keyboard.insertText(')')
  await expect(editor.getByTestId('sql-diagnostic-range')).toHaveCount(0)
})
