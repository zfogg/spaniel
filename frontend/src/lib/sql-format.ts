import { useEffect, useState } from 'react'
import { api } from './api'

export async function formatSQL(sql: string) {
  const result = await api.sql.format({ sql, dialect: 'duckdb' })
  const formatted = result.data.sql
  if (typeof formatted !== 'string') throw new Error('SQL formatter returned an invalid response')
  return formatted
}

// Read-only code views also include locally supplied examples and SQL from
// old telemetry. Editors deliberately never call this hook.
const displayed = new Map<string, Promise<string>>()
export function useFormattedSQL(value: string) {
  const [result, setResult] = useState({ input: '', output: '' })
  useEffect(() => {
    let active = true
    let formatted = displayed.get(value)
    if (!formatted) {
      formatted = formatSQL(value)
      displayed.set(value, formatted)
      if (displayed.size > 128) displayed.delete(displayed.keys().next().value!)
    }
    formatted
      .then((output) => {
        if (active) setResult({ input: value, output })
      })
      .catch(() => {
        displayed.delete(value)
        if (active) setResult({ input: value, output: value })
      })
    return () => {
      active = false
    }
  }, [value])
  return result.input === value ? result.output : value
}
