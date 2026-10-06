// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { LogListPanel } from './LogListPanel'
import type { DashboardPanel } from '@/lib/api'

afterEach(cleanup)
it('shows timestamps and distinct numeric and textual severity colors', () => {
  const rows = [17, 21, 'WARNING', 9].map((severity, index) => ({ severity, timestamp_ns: '1791288000123000000', body: `record ${index}`, trace_id: 'abc' }))
  const { container } = render(<MemoryRouter><LogListPanel panel={{} as DashboardPanel} columns={[]} rows={rows}/></MemoryRouter>)
  for (const [level, color] of [['ERROR', 'red'], ['FATAL', 'purple'], ['WARN', 'yellow'], ['INFO', 'blue']]) {
    expect(screen.getByText(level).className).toContain(`bg-${color}-500/15`)
    expect(screen.getByText(level).className).toContain(`dark:text-${color}-300`)
  }
  expect(container.querySelectorAll('time')).toHaveLength(4)
  expect(container.querySelector('time')?.dateTime).toBe(new Date(1791288000123).toISOString())
  expect(screen.getAllByRole('link')[0].getAttribute('href')).toBe('/logs?traceId=abc')
})
it('does not invent a timestamp for missing or invalid values', () => {
  const { container } = render(<MemoryRouter><LogListPanel panel={{} as DashboardPanel} columns={[]} rows={[{ timestamp_ns: 'invalid' }, {}]}/></MemoryRouter>)
  expect(container.querySelector('time')).toBeNull()
  expect(screen.getAllByText('Timestamp unavailable')).toHaveLength(2)
})
