import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type DashboardPanel } from '@/lib/api'
import { HeatmapPanel } from './HeatmapPanel'
import { EntityListPanel } from './EntityListPanel'
import { LogListPanel } from './LogListPanel'
import { SingleValuePanel } from './SingleValuePanel'
import { SpanListPanel } from './SpanListPanel'
import { TablePanel } from './TablePanel'
import { TimeSeriesPanel } from './TimeSeriesPanel'
import { TraceListPanel } from './TraceListPanel'
import { DeployCorrelationPanel } from './DeployCorrelationPanel'
import type { DashboardPanelRendererProps } from './types'
import { qk } from '@/lib/query'

export function PanelRenderer({ dashboardId, panel, variables = {}, onStatus }: { dashboardId: string; panel: DashboardPanel; variables?: Record<string, string>; onStatus?: (id: string, status: 'loading' | 'ready' | 'error', message?: string) => void }) {
  // Session changes can happen without new telemetry events. Share this small
  // metadata query across panels so even an idle session switch refreshes them.
  const session = useQuery({ queryKey: qk.activeSession(), queryFn: () => api.sessions.getActive().then(response => response.data), enabled: panel.query_sql.includes('$session_id'), refetchInterval: 5000, staleTime: 5000 })
  const result = useQuery({ queryKey: ['dashboard-panel', dashboardId, panel.id, panel.query_sql, panel.display_type, variables, panel.query_sql.includes('$session_id') ? session.data?.id : null], queryFn: ({ signal }) => api.dashboards.preview(dashboardId, { query_sql: panel.query_sql, display_type: panel.display_type, variables }, signal).then(response => response.data), staleTime: 15_000, refetchInterval: 30_000, retry: 1, refetchOnWindowFocus: false })
  let annotationQuery = ''
  try { annotationQuery = (JSON.parse(panel.settings_json) as { annotation_query?: string }).annotation_query ?? '' } catch { /* settings are optional */ }
  const annotations = useQuery({ queryKey: ['dashboard-annotations', dashboardId, panel.id, annotationQuery, variables], queryFn: ({ signal }) => api.dashboards.preview(dashboardId, { query_sql: annotationQuery, display_type: 'table', variables }, signal).then(response => response.data), enabled: panel.display_type === 'deploy_correlation' && Boolean(annotationQuery), staleTime: 15_000, refetchInterval: 30_000, retry: 1, refetchOnWindowFocus: false })
  useEffect(() => { onStatus?.(panel.id, result.isPending ? 'loading' : result.isError ? 'error' : 'ready', result.error instanceof Error ? result.error.message : undefined) }, [onStatus, panel.id, result.isPending, result.isError, result.error])
  if (result.isPending) return <div className="flex min-h-40 items-center justify-center text-xs text-muted-foreground">Loading panel data…</div>
  if (result.isError) return <div role="alert" className="px-1 py-3 text-xs text-danger">Could not load this panel: {result.error.message}</div>
  if (panel.display_type === 'deploy_correlation') return <DeployCorrelationPanel panel={panel} rows={result.data.rows} columns={result.data.columns} annotations={annotations.data?.rows ?? []} annotationError={annotations.error instanceof Error ? annotations.error.message : undefined}/>
  return <PanelResult panel={panel} rows={result.data.rows} columns={result.data.columns}/>
}

// Shared by saved panels and the SQL studio: no separate preview visualization.
export function PanelResult(props: DashboardPanelRendererProps) {
  const { panel, rows } = props
  if (!rows.length) return <div className="flex min-h-40 items-center justify-center text-xs text-muted-foreground">No telemetry matches this query.</div>
  switch (panel.display_type) {
    case 'single_value': return <SingleValuePanel {...props}/>
    case 'time_series': return <TimeSeriesPanel {...props}/>
    case 'table': return <TablePanel {...props}/>
    case 'heatmap': return <HeatmapPanel {...props}/>
    case 'entity_list': return <EntityListPanel {...props}/>
    case 'span_list': return <SpanListPanel {...props}/>
    case 'trace_list': return <TraceListPanel {...props}/>
    case 'log_list': return <LogListPanel {...props}/>
    case 'deploy_correlation': return <DeployCorrelationPanel {...props} annotations={[]}/>
  }
}
