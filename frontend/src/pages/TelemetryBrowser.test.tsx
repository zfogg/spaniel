// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { TelemetryBrowser } from './Dashboards'

afterEach(cleanup)
const catalog = [
  { signal: 'logs', name: 'Matching logs', display_type: 'log_list', query: "SELECT body FROM logs WHERE contains(body, 'needle')" },
  { signal: 'spans', name: 'Matching spans', display_type: 'span_list', query: 'SELECT span_id FROM spans' },
]
describe('TelemetryBrowser', () => {
  it('keeps SQL as wrapping highlighted text and creates only the selected recipe', () => {
    const select = vi.fn(), createPanel = vi.fn()
    const { container } = render(<TelemetryBrowser search="" setSearch={vi.fn()} catalog={catalog} select={select} createPanel={createPanel}/> )
    expect(screen.queryByRole('button', { name: 'Create panel' })).toBeNull()
    expect(container.querySelector('textarea')).toBeNull()
    expect(container.querySelector('code')?.className).toContain('whitespace-pre-wrap')
    expect(container.querySelector('code span')?.textContent).toBe('SELECT')
    fireEvent.click(screen.getByRole('button', { name: /^Matching logs/ }))
    expect(screen.getByRole('button', { name: /^Matching logs/ }).getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(screen.getByRole('button', { name: /^Matching spans/ }))
    expect(screen.getByRole('button', { name: /^Matching logs/ }).getAttribute('aria-pressed')).toBe('false')
    expect(screen.getAllByRole('button', { name: 'Create panel' })).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Create panel' }))
    expect(createPanel).toHaveBeenCalledExactlyOnceWith(catalog[1])
    expect(select).toHaveBeenCalledTimes(2)
  })
  it('does not disguise loading or errors as an empty result', () => {
    const props = { search: '', setSearch: vi.fn(), catalog: [], select: vi.fn(), createPanel: vi.fn() }
    const { rerender } = render(<TelemetryBrowser {...props} loading/>)
    expect(screen.getByRole('status').textContent).toContain('Searching')
    expect(screen.queryByText(/No examples/)).toBeNull()
    rerender(<TelemetryBrowser {...props} error="Request timed out"/>)
    expect(screen.getByRole('alert').textContent).toContain('Request timed out')
    expect(screen.queryByText(/No examples/)).toBeNull()
    rerender(<TelemetryBrowser {...props}/>)
    expect(screen.getByText(/No examples/)).toBeTruthy()
  })
})
