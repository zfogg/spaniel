// @vitest-environment jsdom
import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import ErrorState from './ErrorState'

describe('ErrorState', () => {
  afterEach(cleanup)

  it('renders a headline naming what failed', () => {
    render(<ErrorState what="traces" />)
    expect(screen.getByText("Couldn't load traces")).toBeTruthy()
  })

  it('has role="alert" so screen readers announce it', () => {
    render(<ErrorState what="traces" />)
    expect(document.querySelector('[role="alert"]')).toBeTruthy()
  })

  it('surfaces an Error message verbatim', () => {
    render(<ErrorState what="metrics" error={new Error('500 Internal Server Error')} />)
    expect(screen.getByText('500 Internal Server Error')).toBeTruthy()
  })

  it('surfaces a string error verbatim', () => {
    render(<ErrorState what="logs" error="boom" />)
    expect(screen.getByText('boom')).toBeTruthy()
  })

  it('falls back to a "server unreachable" hint when there is no error detail', () => {
    render(<ErrorState what="spans" />)
    expect(screen.getByText(/Couldn't reach spaniel/)).toBeTruthy()
  })

  it('renders a Retry button that calls onRetry when provided', () => {
    const onRetry = vi.fn()
    render(<ErrorState what="traces" onRetry={onRetry} />)
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('omits the Retry button when onRetry is not provided', () => {
    render(<ErrorState what="traces" />)
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
  })
  it('collapses recursive database errors and retains the original cause', () => {
    const nested =
      'Failed: database has been invalidated because of a previous fatal error. Original error: "'.repeat(
        10000,
      )
    render(
      <ErrorState
        what="this trace"
        error={nested + 'Could not find node in column segment tree' + '"'.repeat(10000)}
        onRetry={() => {}}
      />,
    )
    const alert = screen.getByRole('alert')
    expect(alert.textContent!.length).toBeLessThan(4500)
    expect(screen.getByText(/Could not find node in column segment tree/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
  })
  it('bounds huge ordinary errors', () => {
    render(<ErrorState what="this trace" error={'x'.repeat(100000)} />)
    expect(screen.getByRole('alert').textContent!.length).toBeLessThan(4500)
    expect(screen.getByText(/truncated/)).toBeTruthy()
  })
})
