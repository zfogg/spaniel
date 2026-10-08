// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api, type AlertRule } from '@/lib/api'
import { Inspector } from './AlertInspector'
import { emptyRule } from './alert-model'

vi.mock('@/components/ui/HighlightedCode', () => ({
  SqlCode: ({ value }: { value: string }) => <code>{value}</code>,
  SqlEditor: () => <div />,
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('preserves unsaved edits on refresh and resets the draft when its rule or edit mode changes', () => {
  vi.spyOn(api.alerts, 'events').mockResolvedValue({ data: [], meta: { total: 0 }, silences: [] })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const rule = { ...emptyRule(), id: 'first', name: 'Original' }
  const view = (current: AlertRule, editing = true) => (
    <QueryClientProvider client={client}>
      <Inspector
        rule={current}
        editingFromURL={editing}
        showYaml={() => {}}
        onEditingChange={() => {}}
        onDuplicate={() => {}}
      />
    </QueryClientProvider>
  )
  const { rerender } = render(view(rule))
  fireEvent.change(screen.getByLabelText('Alert title'), { target: { value: 'Unsaved edit' } })
  const refreshed = { ...rule, name: 'Server update', last_evaluated_at: 100 }
  rerender(view(refreshed))
  expect((screen.getByLabelText('Alert title') as HTMLInputElement).value).toBe('Unsaved edit')
  rerender(view(refreshed, false))
  expect(screen.queryByLabelText('Alert title')).toBeNull()
  rerender(view(refreshed))
  expect((screen.getByLabelText('Alert title') as HTMLInputElement).value).toBe('Server update')
  rerender(view({ ...rule, id: 'second', name: 'Second rule' }))
  expect((screen.getByLabelText('Alert title') as HTMLInputElement).value).toBe('Second rule')
})
