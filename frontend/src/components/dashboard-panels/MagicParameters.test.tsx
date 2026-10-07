// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MagicParameters } from './MagicParameters'

afterEach(cleanup)
it('shows accurate sources and types and inserts named parameters', () => {
  const insert = vi.fn()
  render(<MagicParameters insert={insert}/>)
  for (const [name, source, kind] of [
    ['session_id', 'Current session', 'string'],
  ]) {
    const button = screen.getByRole('button', { name: '$' + name + ' ' + source + ' ' + kind })
    fireEvent.click(button)
    expect(insert).toHaveBeenLastCalledWith('$' + name)
  }
  expect(screen.getAllByRole('button')).toHaveLength(1)
})
it('exposes the full field and status semantics in a keyboard-accessible tooltip', async () => {
  render(<MagicParameters insert={vi.fn()}/>)
  fireEvent.focus(screen.getByRole('button', { name: /session_id/ }))
  const tooltip = (await screen.findByText(/active ingestion session ID/)).parentElement!
  expect(tooltip.textContent).toContain('Current session')
})
