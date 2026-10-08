function readSettings(value: string): Record<string, unknown> {
  try {
    return JSON.parse(value) as Record<string, unknown>
  } catch {
    return {}
  }
}

export function RendererSettings({
  display,
  value,
  onChange,
}: {
  display: string
  value: string
  onChange: (value: string) => void
}) {
  const settings = readSettings(value)
  const update = (next: Record<string, unknown>) => onChange(JSON.stringify(next))
  const field = (label: string, key: string, nested = false) => (
    <label className="font-mono text-[10px] text-muted-foreground">
      {label}
      <input
        value={String(
          nested
            ? ((settings.columns as Record<string, string> | undefined)?.[key] ?? '')
            : (settings[key] ?? ''),
        )}
        onChange={(event) =>
          update(
            nested
              ? {
                  ...settings,
                  columns: {
                    ...((settings.columns as Record<string, string>) ?? {}),
                    [key]: event.target.value,
                  },
                }
              : { ...settings, [key]: event.target.value },
          )
        }
        className="mt-1 block w-full rounded border border-input bg-background px-2 py-1 font-sans text-[11px] text-foreground"
      />
    </label>
  )
  if (display === 'deploy_correlation')
    return (
      <div className="grid gap-2 sm:col-span-2 sm:grid-cols-2">
        <p className="sm:col-span-2 font-mono text-[10px] text-muted-foreground">
          Release annotations
        </p>
        {field('Release label', 'annotation_label')}
        {field('Annotation SQL', 'annotation_query')}
      </div>
    )
  if (display === 'entity_list')
    return (
      <div className="grid gap-2 sm:col-span-2 sm:grid-cols-3">
        <p className="sm:col-span-3 font-mono text-[10px] text-muted-foreground">Entity columns</p>
        {field('Label column', 'label', true)}
        {field('Value column', 'value', true)}
        {field('Secondary column', 'secondary', true)}
        {field('Badge column', 'badge', true)}
        {field('Link column', 'link', true)}
        {field('Unit', 'unit')}
      </div>
    )
  if (display === 'time_series' || display === 'heatmap')
    return (
      <div className="grid gap-2 sm:col-span-2 sm:grid-cols-2">
        <label className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground">
          <input
            type="checkbox"
            checked={Boolean(settings.scroll)}
            onChange={(event) => update({ ...settings, scroll: event.target.checked })}
          />{' '}
          Scroll oversized content
        </label>
        {field('Maximum height (px)', 'max_height_px')}
      </div>
    )
  return null
}
