// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { api } from './api'
import { useFormattedSQL } from './sql-format'

vi.mock('./api', () => ({ api: { sql: { format: vi.fn() } } }))
beforeEach(() => {
  vi.mocked(api.sql.format).mockReset()
})
afterEach(cleanup)

it('preserves read-only telemetry without a known dialect', () => {
  const value = String.raw`select 'it\'s select id,name from t \' ok'`
  const { result } = renderHook(() => useFormattedSQL(value))
  expect(result.current).toBe(value)
  expect(api.sql.format).not.toHaveBeenCalled()
})

it('passes the source dialect for read-only MySQL SQL', async () => {
  const value = String.raw`select 'it\'s select id,name from t \' ok'`
  vi.mocked(api.sql.format).mockResolvedValue({ data: { sql: value } } as Awaited<
    ReturnType<typeof api.sql.format>
  >)
  const { result } = renderHook(() => useFormattedSQL(value, 'mysql'))
  await waitFor(() => expect(api.sql.format).toHaveBeenCalledWith({ sql: value, dialect: 'mysql' }))
  expect(result.current).toBe(value)
})

function deferred() {
  let resolve!: (value: { data: { sql: string } }) => void
  const promise = new Promise<{ data: { sql: string } }>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

it('waits for formatted text before updating a read-only view', async () => {
  const pending = deferred()
  vi.mocked(api.sql.format).mockReturnValue(pending.promise as ReturnType<typeof api.sql.format>)
  const { result } = renderHook(() => useFormattedSQL('select await_result', 'duckdb'))
  expect(result.current).toBe('select await_result')
  await act(async () => {
    pending.resolve({ data: { sql: 'select\n  await_result' } })
  })
  await waitFor(() => expect(result.current).toBe('select\n  await_result'))
})

it('discards a late result for a previous query', async () => {
  const first = deferred(),
    second = deferred()
  vi.mocked(api.sql.format)
    .mockReturnValueOnce(first.promise as ReturnType<typeof api.sql.format>)
    .mockReturnValueOnce(second.promise as ReturnType<typeof api.sql.format>)
  const { result, rerender } = renderHook(({ sql }) => useFormattedSQL(sql, 'duckdb'), {
    initialProps: { sql: 'select first_pending' },
  })
  rerender({ sql: 'select second_pending' })
  await act(async () => {
    first.resolve({ data: { sql: 'select\n  first_pending' } })
  })
  expect(result.current).toBe('select second_pending')
  await act(async () => {
    second.resolve({ data: { sql: 'select\n  second_pending' } })
  })
  await waitFor(() => expect(result.current).toBe('select\n  second_pending'))
})

it('keeps the original SQL visible when formatting fails', async () => {
  vi.mocked(api.sql.format).mockRejectedValue(new Error('formatter unavailable'))
  const { result } = renderHook(() => useFormattedSQL('select failure_fallback', 'duckdb'))
  await act(async () => {})
  expect(result.current).toBe('select failure_fallback')
})
