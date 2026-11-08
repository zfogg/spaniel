import { JsonView as RJVLite, allExpanded, darkStyles, defaultStyles } from 'react-json-view-lite'
import 'react-json-view-lite/dist/index.css'
import { useTheme } from 'next-themes'
import { telemetrySQLDialect } from '@/lib/sql-format'
import { TelemetrySQL } from './SQLDiagnostics'

const MAX_ATTRIBUTE_VALUE_LENGTH = 240

function compactLongValues(value: unknown): { value: unknown; truncated: number } {
  if (typeof value === 'string' && value.length > MAX_ATTRIBUTE_VALUE_LENGTH) {
    return {
      value: `${value.slice(0, MAX_ATTRIBUTE_VALUE_LENGTH)}… +${value.length - MAX_ATTRIBUTE_VALUE_LENGTH} chars`,
      truncated: 1,
    }
  }
  if (Array.isArray(value)) {
    let truncated = 0
    const compacted = value.map((item) => {
      const result = compactLongValues(item)
      truncated += result.truncated
      return result.value
    })
    return { value: compacted, truncated }
  }
  if (value && typeof value === 'object') {
    let truncated = 0
    const compacted: Record<string, unknown> = {}
    for (const [key, item] of Object.entries(value)) {
      const result = compactLongValues(item)
      truncated += result.truncated
      compacted[key] = result.value
    }
    return { value: compacted, truncated }
  }
  return { value, truncated: 0 }
}

// Collapsible JSON viewer for span/log/event attributes and resource maps —
// replaces the bespoke key/value tree code in the inspectors. Themed to blend
// into the surrounding panel (transparent container, mono, small) and follows
// the app's light/dark mode. Attribute maps are shallow, so we expand by default.
export default function JsonView({ data, showSQL = true }: { data: unknown; showSQL?: boolean }) {
  const { resolvedTheme } = useTheme()
  const styles = resolvedTheme === 'dark' ? darkStyles : defaultStyles
  const attributes =
    data && typeof data === 'object' && !Array.isArray(data)
      ? (data as Record<string, unknown>)
      : undefined
  const dialect = attributes && telemetrySQLDialect(attributes)
  const statements =
    attributes && dialect
      ? Object.entries(attributes).filter(
          ([key, value]) =>
            (key === 'db.statement' || key === 'db.query.text') && typeof value === 'string',
        )
      : []
  const remaining =
    statements.length && attributes
      ? Object.fromEntries(
          Object.entries(attributes).filter(([key]) => !statements.some(([name]) => name === key)),
        )
      : data
  const compacted = compactLongValues(remaining)
  return (
    <div
      data-testid="json-view"
      className="px-3.5 py-1 font-mono text-[10.5px] leading-[1.5] break-words [&>div]:!bg-transparent"
    >
      {showSQL && attributes && <TelemetrySQL attributes={attributes} />}
      {compacted.truncated > 0 && (
        <div className="mx-3.5 mb-1 rounded border border-[var(--accent)] bg-[var(--accent-bg)] px-2 py-1 font-mono text-[9px] text-[var(--accent-ink)]">
          … {compacted.truncated} long value{compacted.truncated === 1 ? '' : 's'} abbreviated after{' '}
          {MAX_ATTRIBUTE_VALUE_LENGTH} characters
        </div>
      )}
      <RJVLite data={compacted.value as object} shouldExpandNode={allExpanded} style={styles} />
    </div>
  )
}
