// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ReactElement } from 'react'
import RouteErrorBoundary from './RouteErrorBoundary'

function Boom(): ReactElement {
  throw new Error('kaboom')
}

describe('RouteErrorBoundary', () => {
  afterEach(cleanup)

  it('renders a fallback (with the error message) when a child throws', () => {
    // React logs caught errors to console.error — silence it for this case.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(
      <MemoryRouter>
        <RouteErrorBoundary><Boom /></RouteErrorBoundary>
      </MemoryRouter>,
    )
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText(/This view hit an error/i)).toBeTruthy()
    expect(screen.getByText(/kaboom/)).toBeTruthy()
    expect(screen.getByRole('button', { name: /try again/i })).toBeTruthy()
    spy.mockRestore()
  })

  it('renders children normally when they do not throw', () => {
    render(
      <MemoryRouter>
        <RouteErrorBoundary><div>all good</div></RouteErrorBoundary>
      </MemoryRouter>,
    )
    expect(screen.getByText('all good')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })
})
