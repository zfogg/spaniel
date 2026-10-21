// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import NavigationGroups from './NavigationGroups'

afterEach(cleanup)

it.each([
  ['/', 'Telemetry', 'Traces'],
  ['/traces/trace-123?span=span-456', 'Telemetry', 'Traces'],
  ['/spans/span-456', 'Telemetry', 'Spans'],
  ['/logs/log-123', 'Telemetry', 'Logs'],
  ['/metrics/http.duration', 'Telemetry', 'Metrics'],
  ['/dashboards/new', 'Monitor', 'Dashboards'],
  ['/dashboards/dashboard-123', 'Monitor', 'Dashboards'],
  ['/alerts/alert-123', 'Monitor', 'Alerts'],
  ['/coverage/service', 'Tools', 'Coverage'],
  ['/lint/rule', 'Tools', 'Lint'],
])('keeps the parent selected at %s', async (path, group, label) => {
  render(
    <MemoryRouter initialEntries={[path]}>
      <NavigationGroups />
    </MemoryRouter>,
  )
  const trigger = screen.getByRole('button', { name: group })
  expect(trigger.getAttribute('aria-current')).toBe('page')
  fireEvent.keyDown(trigger, { key: 'ArrowDown' })
  const entry = await screen.findByRole('menuitem', { name: label })
  expect(entry.getAttribute('aria-current')).toBe('page')
  expect(entry.classList.contains('active')).toBe(true)
})

it.each(['/docs/database', '/docs/openapi-spec', '/openapi-docs'])(
  'selects the Docs submenu at %s',
  async (path) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <NavigationGroups />
      </MemoryRouter>,
    )
    const trigger = screen.getByRole('button', { name: 'Tools' })
    expect(trigger.getAttribute('aria-current')).toBe('page')
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    expect(
      (await screen.findByRole('menuitem', { name: 'Docs' })).getAttribute('aria-current'),
    ).toBe('page')
  },
)

it('does not match unrelated paths with a shared prefix', () => {
  render(
    <MemoryRouter initialEntries={['/metrics-other']}>
      <NavigationGroups />
    </MemoryRouter>,
  )
  expect(screen.getByRole('button', { name: 'Telemetry' }).hasAttribute('aria-current')).toBe(false)
})
