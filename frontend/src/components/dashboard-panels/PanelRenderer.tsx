import { useQuery } from '@tanstack/react-query'
import { api, type DashboardPanel } from '@/lib/api'
import { HeatmapPanel } from './HeatmapPanel'
import { LogListPanel } from './LogListPanel'
import { SingleValuePanel } from './SingleValuePanel'
import { TablePanel } from './TablePanel'
import { TimeSeriesPanel } from './TimeSeriesPanel'
import { TraceListPanel } from './TraceListPanel'

export function PanelRenderer({ dashboardId, panel, variables = {} }: { dashboardId: string; panel: DashboardPanel; variables?: Record<string, string> }) {
  const result = useQuery({ queryKey: ['dashboard-panel', dashboardId, panel.id, panel.query_sql, panel.display_type, variables], queryFn: () => api.dashboards.preview(dashboardId, { query_sql: panel.query_sql, display_type: panel.display_type, variables }).then(response => response.data), staleTime: 15_000, retry: 1 })
  if (result.isPending) return <div className="flex h-52 items-center justify-center rounded-md border border-dashed border-border bg-muted/20 text-xs text-muted-foreground">Loading panel data…</div>
  if (result.isError) return <div role="alert" className="rounded-md border border-danger/40 bg-danger/5 px-3 py-3 text-xs text-danger">Could not load this panel: {result.error.message}</div>
  if (!result.data.rows.length) return <div className="flex h-52 items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground">No telemetry matches this query.</div>
  const props = { panel, rows: result.data.rows, columns: result.data.columns }
  switch (panel.display_type) {
    case 'single_value': return <SingleValuePanel {...props}/>
    case 'time_series': return <TimeSeriesPanel {...props}/>
    case 'table': return <TablePanel {...props}/>
    case 'heatmap': return <HeatmapPanel {...props}/>
    case 'trace_list': return <TraceListPanel {...props}/>
    case 'log_list': return <LogListPanel {...props}/>
  }
}
