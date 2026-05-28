// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import EmptyState from './EmptyState'

describe('EmptyState', () => {
  afterEach(cleanup)
  it('renders the title and hint', () => {
    render(<EmptyState title="Nothing here" hint="Send some data first." />)
    expect(screen.getByText('Nothing here')).toBeTruthy()
    expect(screen.getByText('Send some data first.')).toBeTruthy()
  })

  it('renders data-testid="empty-state"', () => {
    render(<EmptyState title="T" hint="H" />)
    expect(document.querySelector('[data-testid="empty-state"]')).toBeTruthy()
  })

  it('renders a CTA link with the correct href when provided', () => {
    render(
      <EmptyState
        title="T"
        hint="H"
        cta={{ label: 'Learn more', href: 'https://example.com/docs' }}
      />
    )
    const link = screen.getByRole('link', { name: 'Learn more' })
    expect(link.getAttribute('href')).toBe('https://example.com/docs')
    expect(link.getAttribute('target')).toBe('_blank')
  })

  it('does not render a CTA when omitted', () => {
    render(<EmptyState title="T" hint="H" />)
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('renders the glyph when provided', () => {
    render(
      <EmptyState
        title="T"
        hint="H"
        glyph={<svg data-testid="test-glyph" />}
      />
    )
    expect(document.querySelector('[data-testid="test-glyph"]')).toBeTruthy()
  })

  it('does not render the glyph slot when omitted', () => {
    const { container } = render(<EmptyState title="T" hint="H" />)
    // No extra wrapper div for the glyph
    const glyphWrapper = container.querySelector('.mb-1')
    expect(glyphWrapper).toBeNull()
  })
})
