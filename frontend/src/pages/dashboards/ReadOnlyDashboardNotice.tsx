export function ReadOnlyDashboardNotice({
  copy,
  compact = false,
}: {
  copy: () => void
  compact?: boolean
}) {
  return (
    <section className={`rounded-md border border-border bg-muted/40 p-3 ${compact ? '' : 'my-3'}`}>
      <h2 className="text-sm font-semibold">YAML is authoritative</h2>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">
        This dashboard is loaded from a file. Editing is disabled because the source YAML replaces
        it at startup.
      </p>
      <button
        type="button"
        onClick={copy}
        className="mt-3 rounded border border-accent bg-accent-bg px-2.5 py-1.5 text-xs font-medium text-accent-ink"
      >
        Export as editable copy
      </button>
    </section>
  )
}
