// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { PanelPreview } from './PanelPreview'
import { buildPanelSQL, builderDefaults } from './panel-builder'

afterEach(cleanup)
function chart(display: string, rows: Record<string, unknown>[], settingsJSON = '{}') {
  return render(
    <PanelPreview
      title="Requests"
      query="SELECT"
      display={display}
      settingsJSON={settingsJSON}
      result={{
        querySQL: 'SELECT',
        displayType: display,
        columns: ['label', 'value'],
        rows,
        warnings: [],
      }}
      pending={false}
      error={null}
    />,
  )
}
it.each(['bar', 'pie', 'gauge'])('renders %s in previews using the panel renderer', (kind) => {
  chart(kind, [{ label: 'orders', value: 25 }])
  expect(screen.getByTestId(`${kind}-panel`)).toBeTruthy()
})
it('bars retain negative values and skip null/non-finite data', () => {
  const { container } = chart('bar', [
    { label: 'loss', value: -5 },
    { label: 'profit', value: 10 },
    { label: 'null', value: null },
    { label: 'invalid', value: Infinity },
  ])
  expect(screen.getByRole('img', { name: 'loss: -5' })).toBeTruthy()
  expect(container.querySelectorAll('rect')).toHaveLength(2)
  expect(container.innerHTML).not.toMatch(/NaN|Infinity/)
})
it('pie shows shares and handles zero and negative totals', () => {
  chart('pie', [
    { label: 'orders', value: 25 },
    { label: 'payments', value: 75 },
  ])
  expect(screen.getByText('25 (25%)')).toBeTruthy()
  cleanup()
  chart('pie', [{ value: 0 }])
  expect(screen.getByRole('status').textContent).toContain('No positive')
  cleanup()
  chart('pie', [{ value: -1 }])
  expect(screen.getByRole('alert').textContent).toContain('non-negative')
})
it('gauges clamp the arc, preserve the actual value and use preview settings', () => {
  const { container } = chart('gauge', [{ value: 250 }], '{"min":0,"max":200,"unit":"ms"}')
  expect(screen.getByRole('img').getAttribute('aria-label')).toContain('250 ms; range 0 to 200')
  expect(container.querySelector('[stroke-dasharray]')?.getAttribute('stroke-dasharray')).toBe(
    '1 1',
  )
  expect(screen.getByText('Value is outside the configured range.')).toBeTruthy()
  cleanup()
  chart('gauge', [{ value: 1 }], '{"min":10,"max":0}')
  expect(screen.getByRole('alert')).toBeTruthy()
})
it.each(['bar', 'pie'])('builds grouped label/value SQL for %s', (kind) => {
  expect(buildPanelSQL(kind, builderDefaults)).toContain('service_name AS label, count(*) AS value')
  expect(buildPanelSQL(kind, builderDefaults)).toContain('GROUP BY 1')
})
it('builds scalar SQL for gauges', () => {
  expect(buildPanelSQL('gauge', builderDefaults)).toContain('SELECT count(*) AS value')
  expect(buildPanelSQL('gauge', builderDefaults)).not.toContain('GROUP BY')
})
