import { JsonView as RJVLite, allExpanded, darkStyles, defaultStyles } from 'react-json-view-lite'
import 'react-json-view-lite/dist/index.css'
import { useTheme } from 'next-themes'
import { telemetrySQLDialect } from '@/lib/sql-format'
import { TelemetrySQL } from './SQLDiagnostics'

const MAX_ATTRIBUTE_VALUE_LENGTH = 240

function compactLongValues(value: unknown): { value: unknown; badges: string[] } {
  if (typeof value === 'string' && value.length > MAX_ATTRIBUTE_VALUE_LENGTH) {
    return {
      value: value.slice(0, MAX_ATTRIBUTE_VALUE_LENGTH),
      badges: [`… +${value.length - MAX_ATTRIBUTE_VALUE_LENGTH} chars`],
    }
  }
  if (Array.isArray(value)) {
    let badges: string[] = []
    const compacted = value.map((item) => {
      const result = compactLongValues(item)
      badges = [...badges, ...result.badges]
      return result.value
    })
    return { value: compacted, badges }
  }
  if (value && typeof value === 'object') {
    let badges: string[] = []
    const compacted: Record<string, unknown> = {}
    for (const [key, item] of Object.entries(value)) {
      const result = compactLongValues(item)
      badges = [...badges, ...result.badges]
      compacted[key] = result.value
    }
    return { value: compacted, badges }
  }
  return { value, badges: [] }
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
      {statements.map(([key]) => (
        <div
          key={key}
          className="mx-3.5 mb-1 inline-flex items-center rounded border border-yellow-500/50 bg-yellow-100 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-yellow-900 dark:bg-yellow-950/60 dark:text-yellow-200"
        >
          {key} · formatted below
        </div>
      ))}
      {showSQL && attributes && <TelemetrySQL attributes={attributes} />}
      {compacted.badges.length > 0 && (
        <div className="mx-3.5 mb-1 flex flex-wrap items-center gap-1">
          {compacted.badges.map((badge, index) => (
            <span
              key={`${badge}-${index}`}
              className="inline-flex rounded border border-yellow-500/50 bg-yellow-100 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-yellow-900 dark:bg-yellow-950/60 dark:text-yellow-200"
              title={`Value abbreviated after ${MAX_ATTRIBUTE_VALUE_LENGTH} characters`}
            >
              {badge}
            </span>
          ))}
        </div>
      )}
      <RJVLite data={compacted.value as object} shouldExpandNode={allExpanded} style={styles} />
    </div>
  )
}
