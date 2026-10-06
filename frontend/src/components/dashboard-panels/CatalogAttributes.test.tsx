// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { CatalogAttributes } from './CatalogAttributes'

afterEach(cleanup)

it('shows exact stream dimensions with concise labels and original keys in tooltips', () => {
  const { container } = render(<CatalogAttributes attributes={{ 'server.port': 5173, percentile: 'p95', 'http.route': '/ws', 'server.address': 'localhost', custom: false }}/>)
  expect(container.querySelector('dt')?.textContent).toBe('route')
  expect(screen.getByTitle('server.port: 5173').textContent).toBe('port5173')
  expect(screen.getByTitle('custom: false').textContent).toBe('customfalse')
  expect(screen.queryByText('p95')).toBeNull()
  expect(screen.getByText('/ws')).toBeTruthy()
})

it('does not add an empty row for examples or percentile-only streams', () => {
  const { container, rerender } = render(<CatalogAttributes/>)
  expect(container.textContent).toBe('')
  rerender(<CatalogAttributes attributes={{ percentile: 'p95' }}/>)
  expect(container.querySelector('dl')).toBeNull()
})
