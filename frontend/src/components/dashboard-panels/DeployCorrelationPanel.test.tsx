// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DeployCorrelationPanel } from './DeployCorrelationPanel'

describe('DeployCorrelationPanel', () => {
  it('renders release annotations as compact chips', () => {
    render(
      <DeployCorrelationPanel
        columns={['timestamp_ns', 'value']}
        rows={[{ timestamp_ns: 1, value: 4 }]}
        annotations={[{ timestamp_ns: 1, release: 'v1.2.3' }]}
        panel={{
          id: 'deploy',
          dashboard_id: 'dashboard',
          title: 'Deploys',
          display_type: 'deploy_correlation',
          query_sql: 'SELECT',
          query_version: 1,
          settings_json: '{"annotation_label":"Releases"}',
          layout_json: '{}',
          position: 0,
          updated_at: 0,
        }}
      />,
    )
    expect(screen.getByText('Releases')).toBeTruthy()
    expect(screen.getByText('v1.2.3').className).toContain('truncate')
  })
})
