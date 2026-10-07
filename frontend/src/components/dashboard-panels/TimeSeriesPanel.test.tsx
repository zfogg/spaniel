// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { TimeSeriesPanel } from './TimeSeriesPanel'

afterEach(cleanup)

describe('TimeSeriesPanel', () => {
  it('keeps a dense series fitted to its panel by default', () => {
    const rows = Array.from({ length: 81 }, (_, index) => ({
      timestamp_ns: 1_700_000_000_000_000_000 + index * 60_000_000_000,
      value: index + 1,
    }))
    render(
      <TimeSeriesPanel
        panel={{ settings_json: '{}' } as never}
        rows={rows}
        columns={['timestamp_ns', 'value']}
      />,
    )

    const chart = screen.getByRole('img')
    expect(chart.parentElement?.className).not.toContain('overflow-auto')
    expect(chart.getAttribute('style')).toBeNull()
  })

  it('scrolls only when panel settings request a wider chart', () => {
    render(
      <TimeSeriesPanel
        panel={{ settings_json: '{"min_width_px": 1200}' } as never}
        rows={[{ timestamp_ns: 1_700_000_000_000_000_000, value: 1 }]}
        columns={['timestamp_ns', 'value']}
      />,
    )

    const chart = screen.getByRole('img')
    expect(chart.parentElement?.className).toContain('overflow-auto')
    expect(chart.getAttribute('style')).toContain('min-width')
  })
})
