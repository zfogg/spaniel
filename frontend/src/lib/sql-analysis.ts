import { useEffect, useState } from 'react'
import { api, type AnalyzeSQLInput } from './api'
import type { components } from '@/generated/openapi'

export type SQLAnalysis = components['schemas']['SQLDisplayAnalysis']
export type SQLDiagnostic = components['schemas']['SQLDisplayDiagnostic']

// Responses belong to an exact document and provenance envelope. Never reuse
// an old response after typing, changing spans, or changing displayed SQL.
export function useSQLAnalysis(input?: AnalyzeSQLInput) {
  const key = input ? JSON.stringify(input) : ''
  const [result, setResult] = useState<{ key: string; analysis?: SQLAnalysis; error?: string }>({
    key: '',
  })
  useEffect(() => {
    if (!key) return
    let active = true
    const timer = setTimeout(() => {
      void api.sql.analyze(JSON.parse(key) as AnalyzeSQLInput).then(
        ({ data }) => {
          if (active) setResult({ key, analysis: data })
        },
        () => {
          if (active) setResult({ key, error: 'SQL analysis unavailable' })
        },
      )
    }, 180)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [key])
  return result.key === key ? result : { key }
}

// Ranges are already global UTF-16 positions in this exact display document.
// Bounds and scalar boundaries are checked again before browser decoration.
export function displayRange(d: SQLDiagnostic, text: string): [number, number] | undefined {
  const start = d.display_start,
    end = d.display_end
  if (
    d.mapping_status === 'unavailable' ||
    start === undefined ||
    end === undefined ||
    !Number.isInteger(start) ||
    !Number.isInteger(end) ||
    start < 0 ||
    end < start ||
    end > text.length
  )
    return
  const split = (at: number) =>
    at > 0 &&
    at < text.length &&
    /[\uD800-\uDBFF]/u.test(text[at - 1]) &&
    /[\uDC00-\uDFFF]/u.test(text[at])
  if (split(start) || split(end)) return
  return [start, end]
}
