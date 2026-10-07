// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { EntityListPanel } from './EntityListPanel'

describe('EntityListPanel', () => {
  it('uses configured label, value, secondary, badge, unit, and drill-in columns', () => {
    render(
      <MemoryRouter>
        <EntityListPanel
          columns={['service', 'p95', 'owner', 'health', 'href']}
          rows={[
            {
              service: 'checkout',
              p95: 42,
              owner: 'payments',
              health: 'warning',
              href: '/traces/1',
            },
          ]}
          panel={{
            id: 'entities',
            dashboard_id: 'dashboard',
            title: 'Services',
            display_type: 'entity_list',
            query_sql: 'SELECT',
            query_version: 1,
            settings_json: JSON.stringify({
              columns: {
                label: 'service',
                value: 'p95',
                secondary: 'owner',
                badge: 'health',
                link: 'href',
              },
              unit: 'ms',
            }),
            layout_json: '{}',
            position: 0,
            updated_at: 0,
          }}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'checkout' }).getAttribute('href')).toBe('/traces/1')
    expect(screen.getByText('payments')).toBeTruthy()
    expect(screen.getByText('warning')).toBeTruthy()
    expect(screen.getByText(/42.*ms/)).toBeTruthy()
  })
})
