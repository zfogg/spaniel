import { JsonView as RJVLite, allExpanded, darkStyles, defaultStyles } from 'react-json-view-lite'
import 'react-json-view-lite/dist/index.css'
import { useTheme } from 'next-themes'

// Collapsible JSON viewer for span/log/event attributes and resource maps —
// replaces the bespoke key/value tree code in the inspectors. Themed to blend
// into the surrounding panel (transparent container, mono, small) and follows
// the app's light/dark mode. Attribute maps are shallow, so we expand by default.
export default function JsonView({ data }: { data: unknown }) {
  const { resolvedTheme } = useTheme()
  const styles = resolvedTheme === 'dark' ? darkStyles : defaultStyles
  return (
    <div
      data-testid="json-view"
      className="px-3.5 py-1 font-mono text-[10.5px] leading-[1.5] break-words [&>div]:!bg-transparent"
    >
      <RJVLite data={data as object} shouldExpandNode={allExpanded} style={styles} />
    </div>
  )
}
