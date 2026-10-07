// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { DashboardCanvas } from './DashboardCanvas'
import { movedPanelLayout, panelLayout } from './panel-layout'
import type { DashboardPanel } from '@/lib/api'

const panel: DashboardPanel = {
  id: 'panel',
  dashboard_id: 'dashboard',
  title: 'Latency',
  display_type: 'time_series',
  query_sql: 'SELECT 1 AS value',
  query_version: 1,
  settings_json: '{}',
  layout_json: JSON.stringify({ x: 1, y: 1, w: 6, h: 1 }),
  position: 0,
  updated_at: 0,
}

afterEach(cleanup)

describe('DashboardCanvas', () => {
  it('normalizes legacy placement and persists pointer resizes as grid coordinates', () => {
    expect(panelLayout({ ...panel, layout_json: '{"width":"wide"}' })).toEqual({
      x: 1,
      y: 1,
      w: 12,
      h: 1,
    })
    const commit = vi.fn()
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      width: 1200,
      height: 600,
      top: 0,
      right: 1200,
      bottom: 600,
      left: 0,
      toJSON: () => ({}),
    })
    render(<DashboardCanvas panels={[panel]} onEdit={() => {}} onCommit={commit} />)
    expect(movedPanelLayout({ x: 1, y: 1, w: 6, h: 1 }, { x: 200, y: 112 }, 1200)).toEqual({
      x: 3,
      y: 2,
      w: 6,
      h: 1,
    })
    fireEvent.pointerDown(screen.getByRole('button', { name: 'Resize Latency' }), {
      clientX: 40,
      clientY: 40,
    })
    fireEvent.pointerMove(window, { clientX: 240, clientY: 152 })
    fireEvent.pointerUp(window)
    expect(commit).toHaveBeenLastCalledWith(panel, { x: 1, y: 1, w: 8, h: 2 })
  })

  it('keeps the complete panel query available in the resized card', () => {
    const longQuery = `SELECT\n  duration_ns,\n  service_name\nFROM telemetry_spans\nWHERE duration_ns > 1000000000`
    render(
      <DashboardCanvas
        panels={[{ ...panel, query_sql: longQuery }]}
        onEdit={() => {}}
        onCommit={() => {}}
      />,
    )
    const content = screen.getByText(/duration_ns/)
    expect(content.textContent).toContain(longQuery)
    expect(content.className).toContain('overflow-auto')
    expect(content.className).not.toContain('line-clamp')
  })

  it('preserves explicitly-authored gallery coordinates and dimensions', () => {
    expect(panelLayout({ ...panel, layout_json: '{"x":4,"y":3,"w":8,"h":3}' })).toEqual({
      x: 4,
      y: 3,
      w: 8,
      h: 3,
    })
  })
})
