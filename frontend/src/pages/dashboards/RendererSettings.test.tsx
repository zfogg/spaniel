// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { RendererSettings } from './RendererSettings'

afterEach(cleanup)
it('allows history to be enabled without discarding other panel settings', () => {
  const onChange = vi.fn()
  render(
    <RendererSettings display="single_value" value={'{"unit":"requests"}'} onChange={onChange} />,
  )
  fireEvent.click(screen.getByRole('checkbox', { name: 'Show value history' }))
  expect(JSON.parse(onChange.mock.calls[0][0])).toEqual({ unit: 'requests', show_history: true })
})
