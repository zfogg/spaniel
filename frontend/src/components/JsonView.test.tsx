// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import JsonView from './JsonView'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { sql: { format: vi.fn(), analyze: vi.fn() } } }))
vi.mock('next-themes', () => ({ useTheme: () => ({ resolvedTheme: 'dark' }) }))
beforeEach(() => {
  vi.mocked(api.sql.analyze).mockReset()
})
afterEach(cleanup)

it.each(['postgresql', 'duckdb', 'sqlite', 'mysql', 'clickhouse'] as const)(
  'displays both telemetry SQL attributes as formatted %s code',
  async (dialect) => {
    const statement = `select id,name from ${dialect}_accounts`
    const formatted = `select\n  id,\n  name\nfrom ${dialect}_accounts`
    vi.mocked(api.sql.analyze).mockResolvedValue({
      data: { sql: formatted, status: 'ok', reason: '', diagnostics: [] },
    } as Awaited<ReturnType<typeof api.sql.analyze>>)
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
      ])
    })
    expect(api.sql.analyze).toHaveBeenCalledWith({ sql: statement, dialect, source: 'telemetry' })
    expect(screen.getByText('other:')).toBeTruthy()
    expect(data['db.statement']).toBe(statement)
  },
)

it('uses legacy db.system and withholds invalid statements when analysis fails', async () => {
  vi.mocked(api.sql.analyze).mockRejectedValue(new Error('invalid SQL'))
  const statement = "select 'unfinished-private-value"
  render(<JsonView data={{ 'db.system': 'mysql', 'db.statement': statement }} />)
  await waitFor(() =>
    expect(api.sql.analyze).toHaveBeenCalledWith({
      sql: statement,
      dialect: 'mysql',
      source: 'telemetry',
    }),
  )
  await screen.findByText('SQL analysis unavailable')
  expect(screen.queryByLabelText('SQL query')).toBeNull()
  expect(document.body.textContent).not.toContain('unfinished-private-value')
})

it('maps ingestion fallback diagnostics by reanalyzing the transported document', async () => {
  vi.mocked(api.sql.analyze).mockResolvedValue({
    data: {
      sql: 'SELECT )',
      status: 'ok',
      reason: '',
      diagnostics: [
        {
          code: 'unmatched_close',
          message: 'Unexpected closing delimiter',
          explanation: 'Check delimiters.',
          hint: 'Remove or balance it.',
          line: 1,
          column: 8,
          end_line: 1,
          end_column: 9,
          display_start: 7,
          display_end: 8,
          mapping_status: 'exact',
        },
      ],
    },
  } as Awaited<ReturnType<typeof api.sql.analyze>>)
  render(
    <JsonView
      data={{
        'db.system.name': 'postgresql',
        'db.query.text': 'SELECT )',
        'db.query.diagnostics': [{ code: 'unmatched_close', offset: 7 }],
        'spaniel.db.query.diagnostics_source': 'display',
      }}
    />,
  )
  expect((await screen.findByTestId('sql-diagnostic-range')).textContent).toBe(')')
  expect(api.sql.analyze).toHaveBeenCalledWith({
    sql: 'SELECT )',
    dialect: 'postgresql',
    source: 'telemetry',
  })
})

it.each([undefined, 'mongodb', 'generic'])(
  'keeps unknown dialect %s in the JSON viewer',
  (system) => {
    render(<JsonView data={{ 'db.system.name': system, 'db.statement': 'find accounts' }} />)
    expect(screen.queryByLabelText('SQL query')).toBeNull()
    expect(screen.getByText('"find accounts"')).toBeTruthy()
    expect(api.sql.analyze).not.toHaveBeenCalled()
  },
)
