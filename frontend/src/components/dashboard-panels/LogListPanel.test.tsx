// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { LogListPanel } from './LogListPanel'
import type { DashboardPanel } from '@/lib/api'

afterEach(cleanup)
it('shows timestamps and distinct numeric and textual severity colors', () => {
  const rows = [17, 21, 'WARNING', 9, 5].map((severity, index) => ({
    severity,
    timestamp_ns: '1791288000123000000',
    body: `record ${index}`,
    trace_id: 'abc',
  }))
  const { container } = render(
    <MemoryRouter>
      <LogListPanel panel={{} as DashboardPanel} columns={[]} rows={rows} />
    </MemoryRouter>,
  )
  for (const [level, color] of [
    ['ERROR', 'rgb(127, 29, 29)'],
    ['FATAL', 'rgb(107, 33, 168)'],
    ['WARN', 'rgb(113, 63, 18)'],
    ['INFO', 'rgb(255, 255, 255)'],
    ['DEBUG', 'rgb(30, 58, 138)'],
  ]) {
    expect(getComputedStyle(screen.getByText(level)).backgroundColor).toBe(color)
  }
  expect(container.querySelectorAll('time')).toHaveLength(5)
  expect(container.querySelector('time')?.dateTime).toBe(new Date(1791288000123).toISOString())
  expect(screen.getAllByRole('link')[0].getAttribute('href')).toBe('/logs?traceId=abc')
})
it('does not invent a timestamp for missing or invalid values', () => {
  const { container } = render(
    <MemoryRouter>
      <LogListPanel
        panel={{} as DashboardPanel}
        columns={[]}
        rows={[{ timestamp_ns: 'invalid' }, {}]}
      />
    </MemoryRouter>,
  )
  expect(container.querySelector('time')).toBeNull()
  expect(screen.getAllByText('Timestamp unavailable')).toHaveLength(2)
})
