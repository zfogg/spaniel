import { JsonView as RJVLite, allExpanded, darkStyles, defaultStyles } from 'react-json-view-lite'
import 'react-json-view-lite/dist/index.css'
import { useTheme } from 'next-themes'
import { telemetrySQLDialect } from '@/lib/sql-format'
import { SqlCode } from '@/components/ui/HighlightedCode'

// Collapsible JSON viewer for span/log/event attributes and resource maps —
// replaces the bespoke key/value tree code in the inspectors. Themed to blend
// into the surrounding panel (transparent container, mono, small) and follows
// the app's light/dark mode. Attribute maps are shallow, so we expand by default.
export default function JsonView({ data }: { data: unknown }) {
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
  return (
    <div
      data-testid="json-view"
      className="px-3.5 py-1 font-mono text-[10.5px] leading-[1.5] break-words [&>div]:!bg-transparent"
    >
      <RJVLite data={remaining as object} shouldExpandNode={allExpanded} style={styles} />
      {statements.map(([key, value]) => (
        <div key={key} className="my-2 min-w-0 rounded border border-border p-2">
          <div className="mb-1 text-muted-foreground">{key}</div>
          <SqlCode value={value as string} dialect={dialect} />
        </div>
      ))}
    </div>
  )
}
