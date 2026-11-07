// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import type { DashboardPanel } from '@/lib/api'
import { SingleValuePanel } from './SingleValuePanel'

afterEach(cleanup)
const panel = (enabled: boolean) =>
  ({ settings_json: JSON.stringify({ show_history: enabled }) }) as DashboardPanel
it('shows the observed extrema and a bounded line for enabled history', () => {
  const { container } = render(
    <SingleValuePanel
      panel={panel(true)}
      columns={['value']}
      rows={[{ value: 2 }]}
      history={[
        { timestamp_ms: 1, value: -3 },
        { timestamp_ms: 2, value: 4 },
        { timestamp_ms: 3, value: 2 },
      ]}
    />,
  )
  expect(screen.getByText('max 4')).toBeTruthy()
  expect(screen.getByText('min -3')).toBeTruthy()
  expect(container.querySelector('polyline')?.getAttribute('points')).not.toContain('NaN')
})
it('handles a constant history and hides the chart when disabled', () => {
  const history = [
    { timestamp_ms: 1, value: 0 },
    { timestamp_ms: 2, value: 0 },
  ]
  const { container, rerender } = render(
    <SingleValuePanel
      panel={panel(true)}
      columns={['value']}
      rows={[{ value: 0 }]}
      history={history}
    />,
  )
  expect(container.querySelector('polyline')?.getAttribute('points')).not.toContain('NaN')
  rerender(
    <SingleValuePanel
      panel={panel(false)}
      columns={['value']}
      rows={[{ value: 0 }]}
      history={history}
    />,
  )
  expect(screen.queryByLabelText('Value history')).toBeNull()
})

it('extends the chart with the current live value between saved samples', () => {
  const history = [{ timestamp_ms: Date.now() - 60_000, value: 5 }]
  const { rerender } = render(
    <SingleValuePanel
      panel={panel(true)}
      columns={['value']}
      rows={[{ value: 5 }]}
      history={history}
    />,
  )
  expect(screen.getByText('max 5')).toBeTruthy()
  rerender(
    <SingleValuePanel
      panel={panel(true)}
      columns={['value']}
      rows={[{ value: 12 }]}
      history={history}
    />,
  )
  expect(screen.getByText('max 12')).toBeTruthy()
})

it('scales extreme finite values without invalid coordinates', () => {
  const { container } = render(
    <SingleValuePanel
      panel={panel(true)}
      columns={['value']}
      rows={[{ value: 1e308 }]}
      history={[
        { timestamp_ms: 1, value: -1e308 },
        { timestamp_ms: 2, value: 1e308 },
      ]}
    />,
  )
  const points = container.querySelector('polyline')?.getAttribute('points') ?? ''
  expect(points).not.toContain('NaN')
  expect(points).not.toContain('Infinity')
})

it('explains why nonnumeric values have no history', () => {
  render(<SingleValuePanel panel={panel(true)} columns={['value']} rows={[{ value: 'ready' }]} />)
  expect(screen.getByText('History requires a numeric value.')).toBeTruthy()
})
