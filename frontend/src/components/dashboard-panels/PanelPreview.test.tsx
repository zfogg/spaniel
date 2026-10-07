// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { PanelPreview } from './PanelPreview'
afterEach(cleanup)

const result = {
  querySQL: 'SELECT 42 AS value',
  displayType: 'single_value',
  columns: ['value'],
  rows: [{ value: 42 }],
  warnings: [],
}
it('renders the actual single-value component and marks changed SQL stale', () => {
  const { container, rerender } = render(
    <PanelPreview
      title="Count"
      query={result.querySQL}
      display="single_value"
      result={result}
      pending={false}
      error={null}
    />,
  )
  expect(container.querySelector('data')?.textContent).toBe('42')
  expect(screen.queryByRole('status')).toBeNull()
  rerender(
    <PanelPreview
      title="Count"
      query="SELECT 12 AS value"
      display="single_value"
      result={result}
      pending={false}
      error={null}
    />,
  )
  expect(screen.getByRole('status').textContent).toContain('Run preview again')
})
it('renders chart points using the saved panel renderer', () => {
  render(
    <PanelPreview
      title="Volume"
      query="SELECT"
      display="time_series"
      result={{
        ...result,
        querySQL: 'SELECT',
        displayType: 'time_series',
        columns: ['timestamp_ns', 'value'],
        rows: [
          { timestamp_ns: 1e18, value: 2 },
          { timestamp_ns: 1e18 + 6e10, value: 4 },
        ],
      }}
      pending={false}
      error={null}
    />,
  )
  expect(screen.getByRole('img').getAttribute('aria-label')).toBe('Time series from 2 to 4')
})
it('shows loading and real errors instead of placeholder success', () => {
  const props = { title: 'Count', query: '', display: 'single_value' }
  const { rerender } = render(<PanelPreview {...props} pending error={null} />)
  expect(screen.getByRole('status').textContent).toContain('Running')
  rerender(<PanelPreview {...props} pending={false} error={new Error('Column does not exist')} />)
  expect(screen.getByRole('alert').textContent).toContain('Column does not exist')
})
