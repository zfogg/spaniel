// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { HeatmapPanel } from './HeatmapPanel'

afterEach(cleanup)

describe('HeatmapPanel', () => {
  it('interpolates a purple-to-yellow density ramp without opacity', () => {
    const { container } = render(<HeatmapPanel panel={{} as never} columns={['x', 'value']} rows={[{ x: 1, value: 1 }, { x: 2, value: 100 }]} />)
    const cells = container.querySelectorAll('[title]')

    expect(cells).toHaveLength(2)
    expect(cells[0].getAttribute('style')).toContain('color-mix')
    expect(cells[0].getAttribute('style')).toContain('--heatmap-high')
    expect(cells[0].getAttribute('style')).toContain('--heatmap-low')
    expect(cells[0].getAttribute('style')).not.toContain('opacity')
    expect(cells[1].getAttribute('style')).toContain('100%')
    expect(container.textContent).toContain('Bucket 1')
    expect(container.textContent).toContain('Bucket 2')
    expect(container.textContent).toContain('Frequency')
  })
})
