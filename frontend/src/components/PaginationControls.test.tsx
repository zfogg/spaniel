// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import PaginationControls from './PaginationControls'

afterEach(cleanup)

it('reports the current slice and navigates between pages', () => {
  const onPageChange = vi.fn()
  render(<PaginationControls page={2} pageSize={100} total={250} itemLabel="traces" onPageChange={onPageChange} />)

  expect(screen.getByText('100')).toBeTruthy()
  expect(screen.getByText(/of 250 traces/)).toBeTruthy()
  expect(screen.getByText('page 2 of 3')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Previous traces page' }))
  fireEvent.click(screen.getByRole('button', { name: 'Next traces page' }))
  expect(onPageChange).toHaveBeenNthCalledWith(1, 1)
  expect(onPageChange).toHaveBeenNthCalledWith(2, 3)
})

it('shows the remainder and disables next on the final page', () => {
  render(<PaginationControls page={3} pageSize={100} total={250} itemLabel="traces" onPageChange={() => undefined} />)

  expect(screen.getByText('50')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Next traces page' }) as HTMLButtonElement).disabled).toBe(true)
})
