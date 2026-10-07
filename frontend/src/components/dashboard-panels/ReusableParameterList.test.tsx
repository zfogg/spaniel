// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ReusableParameterList } from './ReusableParameterList'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('requires confirmation and removes only the requested saved parameter', async () => {
  const remove = vi.fn().mockResolvedValue(undefined)
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  const props = {
    variables: [
      {
        dashboard_id: 'd',
        name: 'service',
        source: 'spans.service_name',
        kind: 'string' as const,
        default_value: '',
        options_json: '[]',
      },
    ],
    insert: vi.fn(),
    remove,
  }
  const { rerender } = render(<ReusableParameterList {...props} />)
  fireEvent.click(screen.getByRole('button', { name: 'Delete parameter $service' }))
  expect(confirm).toHaveBeenCalled()
  expect(remove).not.toHaveBeenCalled()
  confirm.mockReturnValue(true)
  fireEvent.click(screen.getByRole('button', { name: 'Delete parameter $service' }))
  await waitFor(() => expect(remove).toHaveBeenCalledExactlyOnceWith('service'))
  rerender(<ReusableParameterList {...props} variables={[]} />)
  expect(screen.queryByRole('button', { name: 'Delete parameter $service' })).toBeNull()
})
it('reports a failed deletion without hiding the saved parameter', async () => {
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  render(
    <ReusableParameterList
      variables={[
        {
          dashboard_id: 'd',
          name: 'service',
          source: 'spans.service_name',
          kind: 'string',
          default_value: '',
          options_json: '[]',
        },
      ]}
      insert={vi.fn()}
      remove={vi.fn().mockRejectedValue(new Error('Offline'))}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Delete parameter $service' }))
  expect((await screen.findByRole('alert')).textContent).toContain('Offline')
  expect(screen.getByRole('button', { name: 'Delete parameter $service' })).toBeTruthy()
})
