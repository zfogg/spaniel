// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import JsonView from './JsonView'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { sql: { format: vi.fn() } } }))
vi.mock('next-themes', () => ({ useTheme: () => ({ resolvedTheme: 'dark' }) }))
beforeEach(() => {
  vi.mocked(api.sql.format).mockReset()
})
afterEach(cleanup)

it.each(['postgresql', 'duckdb', 'sqlite', 'mysql', 'clickhouse'] as const)(
  'displays both telemetry SQL attributes as formatted %s code',
  async (dialect) => {
    const statement = `select id,name from ${dialect}_accounts`
    const formatted = `select\n  id,\n  name\nfrom ${dialect}_accounts`
    vi.mocked(api.sql.format).mockResolvedValue({ data: { sql: formatted } } as Awaited<
      ReturnType<typeof api.sql.format>
    >)
    const data = {
      'db.system.name': dialect,
      'db.statement': statement,
      'db.query.text': statement,
      other: 42,
    }
    render(<JsonView data={data} />)
    await waitFor(() => {
      expect(screen.getAllByLabelText('SQL query').map((node) => node.textContent)).toEqual([
        formatted,
        formatted,
      ])
    })
    expect(api.sql.format).toHaveBeenCalledWith({ sql: statement, dialect })
    expect(screen.getByText('other:')).toBeTruthy()
    expect(data['db.statement']).toBe(statement)
  },
)

it('uses legacy db.system and preserves invalid statements on formatter failure', async () => {
  vi.mocked(api.sql.format).mockRejectedValue(new Error('invalid SQL'))
  const statement = "select 'unfinished"
  render(<JsonView data={{ 'db.system': 'mysql', 'db.statement': statement }} />)
  await waitFor(() =>
    expect(api.sql.format).toHaveBeenCalledWith({ sql: statement, dialect: 'mysql' }),
  )
  expect(screen.getByLabelText('SQL query').textContent).toBe(statement)
})

it.each([undefined, 'mongodb', 'generic'])(
  'keeps unknown dialect %s in the JSON viewer',
  (system) => {
    render(<JsonView data={{ 'db.system.name': system, 'db.statement': 'find accounts' }} />)
    expect(screen.queryByLabelText('SQL query')).toBeNull()
    expect(screen.getByText('"find accounts"')).toBeTruthy()
    expect(api.sql.format).not.toHaveBeenCalled()
  },
)
