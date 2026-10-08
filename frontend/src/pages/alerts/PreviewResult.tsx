export function PreviewResult({
  preview,
}: {
  preview: {
    columns: string[]
    rows: Array<Record<string, unknown>>
    condition?: { kind?: string; operator?: string; value?: number; pattern?: string }
    notification_preview?: Array<{ destination: string; status: string; reason?: string }>
  }
}) {
  const condition = preview.condition
  const threshold = condition?.value
  const matchingRows =
    condition?.kind === 'log_match' && condition.pattern
      ? preview.rows.filter((row) => String(row.message ?? '').includes(condition.pattern ?? ''))
      : []
  const activeSources =
    condition?.kind === 'any_of' || condition?.kind === 'all_of'
      ? preview.rows.filter((row) => row.active === true)
      : []
  const breaches =
    condition?.operator && threshold != null
      ? preview.rows.filter((row) => {
          const value = Number(row.value)
          switch (condition.operator) {
            case '>':
              return value > threshold
            case '>=':
              return value >= threshold
            case '<':
              return value < threshold
            case '<=':
              return value <= threshold
            case '=':
              return value === threshold
            case '!=':
              return value !== threshold
            default:
              return false
          }
        }).length
      : 0
  return (
    <section className="rounded border border-border bg-muted/30 p-3">
      <h3 className="text-sm font-medium">Test result</h3>
      <p className="mt-1 text-xs text-muted-foreground">
        {preview.rows.length} recent rows;{' '}
        {condition?.kind === 'no_data'
          ? preview.rows.length === 0
            ? 'no rows: this rule would fire'
            : 'data is present: this rule would stay resolved'
          : condition?.kind === 'log_match'
            ? `${matchingRows.length} rows contain ${JSON.stringify(condition.pattern ?? '')}; ${condition.operator && threshold != null ? `${matchingRows.length} would breach ${condition.operator} ${threshold}` : matchingRows.length > 0 ? 'this rule would fire' : 'this rule would stay resolved'}`
            : condition?.kind === 'any_of' || condition?.kind === 'all_of'
              ? `${activeSources.length}/${preview.rows.length} source rules firing; ${condition.kind === 'all_of' ? 'all' : 'any'} ${condition.kind === 'all_of' && activeSources.length === preview.rows.length ? 'would fire' : condition.kind === 'any_of' && activeSources.length > 0 ? 'would fire' : 'would stay resolved'}`
              : condition && threshold != null
                ? `${breaches} would breach ${condition.operator} ${threshold}`
                : 'no condition verdict available'}
        . This does not save or notify.
      </p>
      {preview.notification_preview && (
        <p className="mt-1 text-xs text-muted-foreground">
          Delivery:{' '}
          {preview.notification_preview
            .map(
              (item) =>
                `${item.destination} ${item.status}${item.reason ? ` (${item.reason})` : ''}`,
            )
            .join(' · ')}
          . This test never sends a notification.
        </p>
      )}
      <div className="mt-2 max-h-44 overflow-auto">
        <table className="w-full text-left font-mono text-[11px]">
          <thead>
            <tr>
              {preview.columns.map((c) => (
                <th className="pr-3" key={c}>
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {preview.rows.slice(0, 20).map((row, i) => (
              <tr key={i}>
                {preview.columns.map((c) => (
                  <td className="pr-3" key={c}>
                    {String(row[c] ?? '')}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
