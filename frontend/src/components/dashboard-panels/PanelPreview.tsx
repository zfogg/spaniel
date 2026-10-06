import type { DashboardPanel, QueryPreview } from '@/lib/api'
import { PanelResult } from './PanelRenderer'

export type PreviewSnapshot = QueryPreview & { querySQL: string; displayType: string }
export function PanelPreview({ title, query, display, result, pending, error }: { title: string; query: string; display: string; result?: PreviewSnapshot; pending: boolean; error: Error | null }) {
  const stale = result && (result.querySQL !== query || result.displayType !== display)
  const panel: DashboardPanel | undefined = result && { id: 'preview', dashboard_id: '', title, query_sql: result.querySQL, display_type: result.displayType as DashboardPanel['display_type'], query_version: 1, settings_json: '{}', layout_json: '{}', position: 0, updated_at: 0 }
  return <><h3 className="mx-2.5 mb-2 text-sm font-semibold">Preview of your panel</h3><section aria-label="Panel preview" className="mx-2.5 mb-2.5 overflow-hidden rounded-lg border border-border bg-surface">
    <header className="border-b border-border px-4 py-3"><p className="font-mono text-[9px] text-muted-foreground">Preview{result ? ` · ${result.rows.length} rows` : ''}</p><h3 className="mt-1 text-[13px] font-semibold">{title || 'Untitled panel'}</h3></header>
    <div className="px-4 py-3">
      {pending ? <p role="status" className="py-6 text-xs text-muted-foreground">Running read-only query…</p> : error ? <p role="alert" className="text-xs text-danger">Preview failed: {error.message}</p> : result && panel ? <>{stale && <p role="status" className="mb-3 text-xs text-amber-700 dark:text-amber-300">SQL or panel type changed. Run preview again to update these results.</p>}<PanelResult panel={panel} rows={result.rows} columns={result.columns}/>{result.warnings.map(warning => <p key={warning} className="mt-2 text-xs text-muted-foreground">{warning}</p>)}</> : <p className="py-6 text-xs text-muted-foreground">Run preview to render the current SQL using the same component as the saved panel.</p>}
    </div>
  </section></>
}
