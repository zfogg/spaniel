// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { HeatmapPanel } from './HeatmapPanel'

afterEach(cleanup)

describe('HeatmapPanel', () => {
  it('interpolates a purple-to-yellow density ramp without opacity', () => {
    const { container } = render(
      <HeatmapPanel
        panel={{} as never}
        columns={['x', 'value']}
        rows={[
          { x: 1, value: 1 },
          { x: 2, value: 100 },
        ]}
      />,
    )
    const cells = container.querySelectorAll('[data-heatmap-cell]')

    expect(cells).toHaveLength(2)
    expect(cells[0].getAttribute('style')).toContain('color-mix')
    expect(cells[0].getAttribute('style')).toContain('--heatmap-high')
    expect(cells[0].getAttribute('style')).toContain('--heatmap-low')
    expect(cells[0].getAttribute('style')).not.toContain('opacity')
    expect(cells[1].getAttribute('style')).toContain('100%')
    expect(container.textContent).toContain('1')
    expect(container.textContent).toContain('2')
    expect(container.textContent).toContain('Frequency')
  })

  it('renders a sorted two-dimensional matrix, aggregates duplicates and marks missing cells', () => {
    const { container } = render(
      <HeatmapPanel
        panel={{} as never}
        columns={['timestamp_ns', 'bucket_ms', 'value']}
        rows={[
          { timestamp_ns: 120000000000, bucket_ms: 10, value: 4 },
          { timestamp_ns: 60000000000, bucket_ms: 0, value: 0 },
          { timestamp_ns: 120000000000, bucket_ms: 10, value: 2 },
        ]}
      />,
    )
    const cells = container.querySelectorAll('[data-heatmap-cell]')
    expect(cells).toHaveLength(4)
    expect(cells[0].getAttribute('aria-label')).toContain('No data')
    expect(cells[1].getAttribute('aria-label')).toContain('Count: 6')
    expect(cells[2].getAttribute('aria-label')).toContain('Count: 0')
    expect(container.querySelector('svg')?.getAttribute('aria-label')).toContain(
      '2 time buckets by 2 duration buckets',
    )
    expect(container.textContent).toContain('10 ms')
  })
})
