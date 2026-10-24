import { useEffect, useState } from 'react'
import { api } from './api'

export type SQLDialect = NonNullable<Parameters<typeof api.sql.format>[0]['dialect']>

export function telemetrySQLDialect(attributes: Record<string, unknown>): SQLDialect | undefined {
  for (const key of ['db.system.name', 'db.system']) {
    const system = attributes[key]
    switch (system) {
      case 'postgresql':
      case 'duckdb':
      case 'sqlite':
      case 'mysql':
      case 'clickhouse':
        return system
    }
  }
}

export async function formatSQL(sql: string, dialect: SQLDialect = 'duckdb') {
  const result = await api.sql.format({ sql, dialect })
  const formatted = result.data.sql
  if (typeof formatted !== 'string') throw new Error('SQL formatter returned an invalid response')
  return formatted
}

// Callers must identify the source dialect. Unknown telemetry remains intact;
// Spaniel's own dashboard and alert queries explicitly use DuckDB.
const displayed = new Map<string, string>()
export function useFormattedSQL(value: string, dialect?: SQLDialect) {
  const [result, setResult] = useState({ input: '', output: '' })
  useEffect(() => {
    let active = true
    if (!dialect) return
    const cacheKey = JSON.stringify([dialect, value])
    async function update() {
      try {
        const output = displayed.get(cacheKey) ?? (await formatSQL(value, dialect))
        displayed.set(cacheKey, output)
        if (displayed.size > 128) displayed.delete(displayed.keys().next().value!)
        if (active) setResult({ input: cacheKey, output })
      } catch {
        if (active) setResult({ input: cacheKey, output: value })
      }
    }
    void update()
    return () => {
      active = false
    }
  }, [value, dialect])
  return dialect && result.input === JSON.stringify([dialect, value]) ? result.output : value
}
