// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TimeSeriesPanel } from './TimeSeriesPanel'

describe('TimeSeriesPanel', () => {
  it('scrolls a dense series instead of clipping its widened chart', () => {
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
    expect(chart.parentElement?.className).toContain('overflow-auto')
    expect(chart.getAttribute('style')).toContain('min-width')
  })
})
