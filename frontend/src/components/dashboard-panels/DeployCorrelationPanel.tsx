import { timeLabel } from './format'
import { TimeSeriesPanel } from './TimeSeriesPanel'
import type { DashboardPanelRendererProps, PanelRow } from './types'

export function DeployCorrelationPanel(props: DashboardPanelRendererProps & { annotations: PanelRow[]; annotationError?: string }) {
  let label = 'Releases'
  try { label = (JSON.parse(props.panel.settings_json) as { annotation_label?: string }).annotation_label || label } catch { /* use default */ }
  return <div className="space-y-3"><TimeSeriesPanel {...props} annotations={props.annotations}/><section className="border-t border-border pt-3"><div className="mb-2 flex items-center justify-between"><h3 className="text-xs font-semibold">{label}</h3><span className="text-[10px] text-muted-foreground">{props.annotations.length} event{props.annotations.length === 1 ? '' : 's'}</span></div>{props.annotationError ? <p className="text-xs text-danger">Could not load {label.toLowerCase()}: {props.annotationError}</p> : props.annotations.length ? <ul className="space-y-1.5">{props.annotations.slice(0, 8).map((event, index) => <li key={index} className="flex gap-2 text-xs"><time className="shrink-0 font-mono text-[10px] text-muted-foreground">{timeLabel(event.timestamp ?? event.timestamp_ns ?? event.time)}</time><span>{String(event.label ?? event.release ?? event.version ?? event.name ?? 'Release')}</span></li>)}</ul> : <p className="text-xs text-muted-foreground">No release source or events are configured for this time window.</p>}</section></div>
}

