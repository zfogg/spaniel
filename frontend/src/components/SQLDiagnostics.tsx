import { useMemo } from 'react'
import { sql } from '@codemirror/lang-sql'
import { tags, tagHighlighter, highlightTree } from '@lezer/highlight'
import { displayRange, useSQLAnalysis, type SQLDiagnostic } from '@/lib/sql-analysis'
import { telemetrySQLDialect } from '@/lib/sql-format'

const staticSQL = sql().language
const staticHighlight = tagHighlighter([
  { tag: tags.keyword, class: 'font-bold text-[var(--sql-keyword)]' },
  { tag: [tags.string, tags.special(tags.string)], class: 'text-[var(--sql-string)]' },
  { tag: tags.number, class: 'text-[var(--sql-number)]' },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    class: 'italic text-[var(--sql-comment)]',
  },
  { tag: [tags.operatorKeyword, tags.operator], class: 'text-[var(--sql-operator)]' },
])

export function SQLDiagnosticList({ diagnostics }: { diagnostics: SQLDiagnostic[] }) {
  return (
    <ul aria-label="SQL diagnostics" className="mt-2 space-y-2 text-xs">
      {diagnostics.map((d, i) => (
        <li
          key={i}
          data-testid="sql-diagnostic"
          className="rounded border border-destructive/40 p-2"
        >
          <strong>{d.code}</strong>: {d.message}
          <div>
            Original line {d.line}, column {d.column}–{d.end_line}:{d.end_column}.
          </div>
          <div>
            {d.explanation} {d.hint}
          </div>
          {d.mapping_status === 'approximate' && (
            <div>Approximate location: source text was replaced or removed.</div>
          )}
          {d.mapping_status === 'unavailable' && (
            <div>Underline unavailable: no safe source mapping for the displayed SQL.</div>
          )}
        </li>
      ))}
    </ul>
  )
}

export function DiagnosticSQL({
  text,
  diagnostics,
}: {
  text: string
  diagnostics: SQLDiagnostic[]
}) {
  const content = useMemo(() => {
    const ranges = diagnostics.flatMap((d) => {
      const r = displayRange(d, text)
      return r ? [{ start: r[0], end: r[1], d }] : []
    })
    const syntaxRanges: { start: number; end: number; className: string }[] = []
    highlightTree(staticSQL.parser.parse(text), staticHighlight, (start, end, className) => {
      syntaxRanges.push({ start, end, className })
    })
    const points = [
      ...new Set([
        0,
        text.length,
        ...ranges.flatMap((r) => [r.start, r.end]),
        ...syntaxRanges.flatMap((r) => [r.start, r.end]),
      ]),
    ].sort((a, b) => a - b)
    return points.flatMap((point, i) => {
      const anchors = ranges
        .filter((r) => r.start === point && r.end === point)
        .map((r, n) => (
          <span
            key={`anchor-${point}-${n}`}
            data-testid="sql-diagnostic-anchor"
            title={r.d.message}
            aria-label={r.d.code}
            className="border-l-2 border-destructive"
          >
            &#8203;
          </span>
        ))
      const end = points[i + 1]
      if (end === undefined) return anchors
      const active = ranges.filter((r) => r.start < end && r.end > point)
      const part = text.slice(point, end)
      const token = syntaxRanges.find((r) => r.start <= point && r.end >= end)
      const content = token ? <span className={token.className}>{part}</span> : part
      return [
        ...anchors,
        active.length ? (
          <mark
            key={point}
            data-testid="sql-diagnostic-range"
            data-mapping-status={
              active.some((r) => r.d.mapping_status === 'approximate') ? 'approximate' : 'exact'
            }
            title={active.map((r) => r.d.message).join('; ')}
            className="bg-transparent text-inherit underline decoration-wavy decoration-destructive underline-offset-4"
          >
            {content}
          </mark>
        ) : (
          content
        ),
      ]
    })
  }, [text, diagnostics])
  return (
    <div
      aria-label="SQL query"
      className="whitespace-pre-wrap font-mono text-xs [overflow-wrap:anywhere]"
    >
      {content}
    </div>
  )
}

const reasons: Record<string, string> = {
  policy: 'SQL text omitted by instrumentation policy.',
  malformed: 'SQL text omitted because the input was malformed.',
  analysis_limit: 'SQL text omitted because analysis reached a resource limit.',
  analysis_error: 'SQL text omitted because analysis failed.',
  empty: 'No SQL text was supplied.',
  cancelled: 'SQL analysis was cancelled.',
}

export function TelemetrySQL({ attributes }: { attributes: Record<string, unknown> }) {
  const query = attributes['db.query.text'] ?? attributes['db.statement']
  const text = typeof query === 'string' ? query : ''
  const dialect = telemetrySQLDialect(attributes)
  const diagnostics = attributes['db.query.diagnostics']
  // Ingestion fallback diagnostics refer to this transported text. Reanalyze
  // that document so the endpoint can derive its own safe display mappings.
  const localDiagnostics = attributes['spaniel.db.query.diagnostics_source'] === 'display'
  const reason = attributes['gotel.db.query.text_reason']
  const rejected = attributes['spaniel.db.query.diagnostics_status'] === 'invalid'
  const relevant =
    dialect &&
    (rejected ||
      text ||
      (Array.isArray(diagnostics) && diagnostics.length > 0) ||
      typeof reason === 'string')
  const { analysis, error } = useSQLAnalysis(
    relevant
      ? {
          sql: text,
          dialect,
          source: 'telemetry',
          ...(!localDiagnostics && Array.isArray(diagnostics)
            ? { diagnostics: diagnostics as Record<string, unknown>[] }
            : {}),
          ...(typeof attributes['gotel.db.query.source_map'] === 'string'
            ? { source_map: attributes['gotel.db.query.source_map'] }
            : {}),
          ...(typeof attributes['gotel.db.query.original_length'] === 'number'
            ? { original_length: attributes['gotel.db.query.original_length'] }
            : {}),
          ...(typeof attributes['gotel.db.query.diagnostic_schema_version'] === 'number'
            ? { diagnostic_schema_version: attributes['gotel.db.query.diagnostic_schema_version'] }
            : {}),
        }
      : undefined,
  )
  if (!relevant) return null
  return (
    <section aria-label="SQL analysis" className="my-2 min-w-0 rounded border border-border p-2">
      {rejected && (
        <p role="alert">
          SQL diagnostic metadata was rejected as invalid or unsupported. The original SQL is
          unavailable for reanalysis.
        </p>
      )}
      {typeof reason === 'string' && (
        <p data-testid="sql-text-reason">
          {reasons[reason] ?? 'SQL text unavailable (unknown omission reason).'}
        </p>
      )}
      {analysis?.sql && <DiagnosticSQL text={analysis.sql} diagnostics={analysis.diagnostics} />}
      {analysis && <SQLDiagnosticList diagnostics={analysis.diagnostics} />}
      {error && <p>{error}</p>}
      {!analysis && !error && <p className="text-muted-foreground">Analyzing SQL…</p>}
    </section>
  )
}
