// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { DiagnosticSQL, SQLDiagnosticList } from './SQLDiagnostics'
import { displayRange, type SQLDiagnostic } from '@/lib/sql-analysis'

afterEach(cleanup)
const diagnostic = (overrides: Partial<SQLDiagnostic> = {}): SQLDiagnostic => ({
  code: 'unmatched_close',
  message: 'Unexpected closing delimiter',
  explanation: 'Check delimiters.',
  hint: 'Remove or balance it.',
  line: 2,
  column: 8,
  end_line: 2,
  end_column: 9,
  mapping_status: 'exact',
  ...overrides,
})

it('decorates multiple overlapping UTF-16 ranges without shifting Unicode or CRLF', () => {
  const text = '😀é\r\nSELECT (1))'
  const start = text.indexOf(')')
  const ds = [
    diagnostic({ display_start: start, display_end: start + 1 }),
    diagnostic({ display_start: start, display_end: start + 2 }),
  ]
  render(<DiagnosticSQL text={text} diagnostics={ds} />)
  const query = screen.getByLabelText('SQL query')
  expect(query.textContent).toBe(text)
  expect(screen.getAllByTestId('sql-diagnostic-range').map((node) => node.textContent)).toEqual([
    ')',
    ')',
  ])
})

it('shows a deletion anchor and explains approximate and unavailable source ranges', () => {
  const ds = [
    diagnostic({ display_start: 7, display_end: 7, mapping_status: 'approximate' }),
    diagnostic({ mapping_status: 'unavailable' }),
  ]
  render(
    <>
      <DiagnosticSQL text="SELECT ?" diagnostics={ds} />
      <SQLDiagnosticList diagnostics={ds} />
    </>,
  )
  expect(screen.getAllByTestId('sql-diagnostic-anchor')).toHaveLength(1)
  expect(screen.queryByTestId('sql-diagnostic-range')).toBeNull()
  expect(screen.getByText(/Approximate location/)).toBeTruthy()
  expect(screen.getByText(/Underline unavailable/)).toBeTruthy()
})

it('rejects split surrogates and out-of-bounds or unavailable mappings', () => {
  for (const d of [
    diagnostic({ display_start: 1, display_end: 2 }),
    diagnostic({ display_start: 0, display_end: 9 }),
    diagnostic({ display_start: 0, display_end: 2, mapping_status: 'unavailable' }),
  ])
    expect(displayRange(d, '😀é')).toBeUndefined()
  expect(displayRange(diagnostic({ display_start: 0, display_end: 2 }), '😀é')).toEqual([0, 2])
  expect(displayRange(diagnostic({ display_start: 2, display_end: 4 }), '😀é')).toEqual([2, 4])
})
