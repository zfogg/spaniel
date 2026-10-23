import { expect, test, type WebSocketRoute } from '@playwright/test'
import { emptyRule } from '../src/pages/alerts/alert-model'

for (const kind of ['dashboard', 'alert'] as const) {
  test(`${kind} updates live and displays an unread badge without navigation`, async ({ page }) => {
    const sockets: WebSocketRoute[] = []
    await page.routeWebSocket('**/ws', (socket) => sockets.push(socket))
    let updated = false
    let name = 'Before file edit'
    let revision = 'before'
    const definition = () => ({
      ...(kind === 'alert'
        ? emptyRule()
        : { panels: [], variables: [], description: '', created_at: 1 }),
      id: 'file-definition',
      name,
      source_file: `${kind}.yaml`,
      source_hash: revision,
      source_file_modified: false,
      updated_at: 1,
      updated,
    })
    await page.route(/^https?:\/\/[^/]+\/api\//, async (route) => {
      const path = new URL(route.request().url()).pathname
      let data: unknown = []
      if (path === '/api/dashboards') data = kind === 'dashboard' ? [definition()] : []
      else if (path === '/api/alerts')
        data = {
          items: kind === 'alert' ? [definition()] : [],
          summary: { rule_counts: {}, instance_counts: {} },
        }
      else if (path.endsWith('/file-definition')) {
        data = definition()
        updated = false
      } else if (path === '/api/sessions/active') data = { id: 'test', label: 'Test' }
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
    await page.goto(kind === 'dashboard' ? '/dashboards' : '/alerts')
    await expect(page.getByRole('heading', { name: 'Before file edit', exact: true })).toBeVisible()
    await expect.poll(() => sockets.length).toBeGreaterThan(0)
    const send = (status: 'updating' | 'updated') =>
      sockets.forEach((socket) =>
        socket.send(
          JSON.stringify({
            type: 'definition_sync',
            timestamp_ns: Date.now() * 1e6,
            payload: { kind, status },
          }),
        ),
      )
    send('updating')
    await expect(
      page.getByText(`Updating ${kind === 'dashboard' ? 'dashboards' : 'alerts'} from YAML…`, {
        exact: true,
      }),
    ).toBeVisible()
    updated = true
    name = 'After file edit'
    revision = 'after'
    send('updated')
    await expect(page.getByRole('heading', { name: /After file edit/ })).toBeVisible()
    await expect(page.getByText('Updated', { exact: true }).first()).toBeVisible()
    await expect(
      page.getByText(`${kind === 'dashboard' ? 'Dashboards' : 'Alerts'} updated from YAML`, {
        exact: true,
      }),
    ).toBeVisible()
  })
}
