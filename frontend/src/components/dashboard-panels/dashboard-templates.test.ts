import { expect, it } from 'vitest'
import { dashboardTemplates } from './dashboard-templates'

it('offers None plus complete, uniquely titled eight-type dashboards', () => {
  expect(dashboardTemplates[0].id).toBe('none')
  expect(dashboardTemplates[0].panels).toEqual([])
  for (const template of dashboardTemplates.slice(1)) {
    expect(template.panels.length).toBeGreaterThanOrEqual(6)
    expect(template.panels.length).toBeLessThanOrEqual(10)
    expect(new Set(template.panels.map(p => p.display_type)).size).toBe(8)
    expect(new Set(template.panels.map(p => p.title)).size).toBe(template.panels.length)
    for (const panel of template.panels) {
      expect(panel.title.length).toBeLessThanOrEqual(160)
      expect(panel.query_sql).toMatch(/^(SELECT|WITH) /)
      expect(panel.query_sql).not.toContain('telemetry_')
      expect(panel.query_sql).toContain('3600000000000')
    }
  }
})

it.skipIf(!import.meta.env.SPANIEL_VERIFY_URL)('executes all template queries against read-only DuckDB', async () => {
  const base = import.meta.env.SPANIEL_VERIFY_URL
  const dashboards = await fetch(`${base}/api/dashboards`).then(r => r.json())
  const id = dashboards.data[0]?.id
  expect(id, 'A saved dashboard is required for the read-only preview endpoint').toBeTruthy()
  for (const template of dashboardTemplates.slice(1)) {
    for (const panel of template.panels) {
      const response = await fetch(`${base}/api/dashboards/${id}/query-preview`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query_sql: panel.query_sql, display_type: panel.display_type }),
      })
      expect(response.ok, `${template.name} / ${panel.title}: ${await response.text()}`).toBe(true)
    }
  }
}, 120_000)
