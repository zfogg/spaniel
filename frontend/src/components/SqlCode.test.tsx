// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { SqlCode } from './SqlCode'

afterEach(cleanup)
it('highlights complete SQL in a wrapping div without an editor', () => {
  const value = "SELECT 'hello' AS name, 42 AS value\n-- comment\nFROM spans"
  render(<SqlCode value={value}/> )
  const code = screen.getByLabelText('SQL query')
  expect(code.tagName).toBe('DIV')
  expect(code.textContent).toBe(value)
  expect(code.className).toContain('whitespace-pre-wrap')
  expect(code.querySelector('textarea, [contenteditable], .cm-editor')).toBeNull()
  expect(code.querySelector('[class*="--sql-keyword"]')?.textContent).toBe('SELECT')
  expect(code.querySelector('[class*="--sql-string"]')?.textContent).toBe("'hello'")
  expect(code.querySelector('[class*="--sql-number"]')?.textContent).toBe('42')
  expect(code.querySelector('[class*="--sql-comment"]')?.textContent).toBe('-- comment')
})
