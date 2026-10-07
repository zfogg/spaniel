import { BookOpen } from 'lucide-react'
import { Link } from 'react-router-dom'

// A normal link deliberately preserves browser history, so an author returns
// to the exact unsaved composer draft with Back.
export function SchemaHelpLink() {
  return (
    <Link
      to="/docs/database-schema"
      className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-[10px] font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
      title="Open the telemetry schema, SQL examples, and parameter rules"
    >
      <BookOpen size={12} />
      Schema &amp; SQL
    </Link>
  )
}
