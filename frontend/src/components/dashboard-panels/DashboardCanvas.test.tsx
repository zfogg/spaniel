// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { DashboardCanvas, panelLayout } from './DashboardCanvas'
import type { DashboardPanel } from '@/lib/api'

const panel: DashboardPanel = { id: 'panel', dashboard_id: 'dashboard', title: 'Latency', display_type: 'time_series', query_sql: 'SELECT 1 AS value', query_version: 1, settings_json: '{}', layout_json: JSON.stringify({ x: 1, y: 1, w: 6, h: 1 }), position: 0, updated_at: 0 }

afterEach(cleanup)

describe('DashboardCanvas', () => {
  it('normalizes legacy placement and persists pointer moves and resizes as grid coordinates', () => {
    expect(panelLayout({ ...panel, layout_json: '{"width":"wide"}' })).toEqual({ x: 1, y: 1, w: 12, h: 1 })
    const commit = vi.fn()
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, width: 1200, height: 600, top: 0, right: 1200, bottom: 600, left: 0, toJSON: () => ({}) })
    render(<DashboardCanvas panels={[panel]} onEdit={() => {}} onCommit={commit}/>)
    fireEvent.pointerDown(screen.getByRole('button', { name: 'Move Latency' }), { clientX: 40, clientY: 40 })
    fireEvent.pointerMove(window, { clientX: 240, clientY: 152 })
    fireEvent.pointerUp(window)
    expect(commit).toHaveBeenLastCalledWith(panel, { x: 3, y: 2, w: 6, h: 1 })
    fireEvent.pointerDown(screen.getByRole('button', { name: 'Resize Latency' }), { clientX: 40, clientY: 40 })
    fireEvent.pointerMove(window, { clientX: 240, clientY: 152 })
    fireEvent.pointerUp(window)
    expect(commit).toHaveBeenLastCalledWith(panel, { x: 3, y: 2, w: 8, h: 2 })
  })
})

