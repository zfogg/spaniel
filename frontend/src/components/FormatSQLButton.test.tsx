// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FormatSQLButton } from './FormatSQLButton'
import { formatSQL } from '@/lib/sql-format'

vi.mock('@/lib/sql-format', () => ({ formatSQL: vi.fn() }))
beforeEach(() => {
  vi.mocked(formatSQL).mockReset()
})
afterEach(cleanup)

describe('Format SQL', () => {
  it('changes the editor only after an explicit click', async () => {
    const change = vi.fn()
    vi.mocked(formatSQL).mockResolvedValue('select\n  id')
    render(<FormatSQLButton value="select id" onChange={change} />)
    expect(formatSQL).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Format SQL' }))
    await waitFor(() => expect(change).toHaveBeenCalledWith('select\n  id'))
  })
  it('preserves edits made while formatting is in flight', async () => {
    let resolve!: (value: string) => void
    vi.mocked(formatSQL).mockReturnValue(
      new Promise((done) => {
        resolve = done
      }),
    )
    const change = vi.fn()
    const view = render(<FormatSQLButton value="select id" onChange={change} />)
    fireEvent.click(screen.getByRole('button', { name: 'Format SQL' }))
    view.rerender(<FormatSQLButton value="select id,name" onChange={change} />)
    resolve('select\n  id')
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'Format SQL' }) as HTMLButtonElement).disabled,
      ).toBe(false),
    )
    expect(change).not.toHaveBeenCalled()
  })
  it('shows formatting errors without replacing the input', async () => {
    vi.mocked(formatSQL).mockRejectedValue(new Error('Unclosed string'))
    const change = vi.fn()
    render(<FormatSQLButton value="select '" onChange={change} />)
    fireEvent.click(screen.getByRole('button', { name: 'Format SQL' }))
    expect((await screen.findByRole('alert')).textContent).toContain('Unclosed string')
    expect(change).not.toHaveBeenCalled()
  })
})
