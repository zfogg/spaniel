// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { DefinitionUpdatedBadge, UpdatedBadge } from './DefinitionUpdatedBadge'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('shows a blue badge only for an unread revision', () => {
  const view = render(<UpdatedBadge updated />)
  expect(screen.getByText('Updated').className).toContain('text-blue-700')
  view.rerender(<UpdatedBadge updated={false} />)
  expect(screen.queryByText('Updated')).toBeNull()
})
it('acknowledges detail access and keeps the badge for the first visit', async () => {
  const get = vi
    .spyOn(api.dashboards, 'get')
    .mockResolvedValue({ data: {} as never, meta: { total: 1 } })
  const qc = new QueryClient()
  const invalidate = vi.spyOn(qc, 'invalidateQueries')
  const view = render(
    <QueryClientProvider client={qc}>
      <DefinitionUpdatedBadge kind="dashboard" id="watched" revision="one" updated />
    </QueryClientProvider>,
  )
  await waitFor(() => expect(get).toHaveBeenCalledWith('watched'))
  view.rerender(
    <QueryClientProvider client={qc}>
      <DefinitionUpdatedBadge kind="dashboard" id="watched" revision="one" updated={false} />
    </QueryClientProvider>,
  )
  expect(screen.getByText('Updated')).toBeTruthy()
  view.unmount()
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboards'] })
})
