import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LayoutDashboard, Plus, Variable, Pencil, ArrowUp, ArrowDown } from 'lucide-react'
import { api, type Dashboard, type DashboardPanel, type DashboardVariable, type QueryCatalogEntry } from '@/lib/api'
import { qk } from '@/lib/query'
import { useDebouncedSave } from '@/lib/useDebouncedSave'
import { toast } from 'sonner'
import { PanelRenderer } from '@/components/dashboard-panels/PanelRenderer'
import { CatalogAttributes } from '@/components/dashboard-panels/CatalogAttributes'
import { PanelBuilderControls } from '@/components/dashboard-panels/PanelBuilderControls'
import { DraftPanelEntry } from '@/components/dashboard-panels/DraftPanelEntry'
import { DashboardCanvas, type PanelLayout } from '@/components/dashboard-panels/DashboardCanvas'
import { MagicParameters } from '@/components/dashboard-panels/MagicParameters'
import { ReusableParameterList } from '@/components/dashboard-panels/ReusableParameterList'
import { PanelPreview, type PreviewSnapshot } from '@/components/dashboard-panels/PanelPreview'
import { NewDashboardStarter } from '@/components/dashboard-panels/NewDashboardStarter'
import { DashboardList } from '@/components/dashboard-panels/DashboardList'
import { dashboardTemplates } from '@/components/dashboard-panels/dashboard-templates'
import { SqlCode, SqlEditor } from '@/components/SqlCode'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const displays = ['single_value', 'time_series', 'table', 'heatmap', 'entity_list', 'trace_list', 'span_list', 'log_list', 'deploy_correlation'] as const
const localKey = 'spaniel.local-dashboards'
const variablePresets: Array<Pick<DashboardVariable, 'name' | 'kind' | 'source' | 'default_value'>> = [
  { name: 'service', kind: 'string', source: 'telemetry_spans.service_name', default_value: '' },
  { name: 'operation', kind: 'string', source: 'telemetry_spans.name', default_value: '' },
  { name: 'status_code', kind: 'number', source: 'telemetry_spans.status_code', default_value: '' },
  { name: 'severity', kind: 'enum', source: 'telemetry_logs.severity', default_value: '' },
  { name: 'trace_id', kind: 'string', source: 'telemetry_spans.trace_id', default_value: '' },
  { name: 'min_duration_ms', kind: 'number', source: 'duration_ns / 1000000', default_value: '0' },
]

const panelRecipes = [
  { type: 'single_value', icon: '#', label: 'Single value', description: 'One current number', fields: [['Aggregation', 'count(*)'], ['Filter', 'all telemetry']], hint: 'Use one scalar value; alias it as value.', shape: [['value', 'number']] },
  { type: 'time_series', icon: '⌁', label: 'Time series', description: 'Value over time', fields: [['Measure', 'count(*)'], ['Interval', '1 minute']], hint: 'Return a timestamp and numeric value for each point.', shape: [['timestamp', 'timestamp'], ['value', 'number']] },
  { type: 'table', icon: '▤', label: 'Table', description: 'Ranked records', fields: [['Sort by', 'duration descending'], ['Limit', '100 rows']], hint: 'Return named columns; preserve a stable order in SQL.', shape: [['any named columns', 'record']] },
  { type: 'heatmap', icon: '▦', label: 'Heatmap', description: 'Distribution', fields: [['Bucket', 'duration'], ['Aggregate', 'count(*)']], hint: 'Return time, duration buckets, and a count for each cell.', shape: [['timestamp_ns', 'time'], ['bucket_ms', 'duration'], ['value', 'count']] },
  { type: 'entity_list', icon: '☷', label: 'Entity list', description: 'Labels, bars, states', fields: [['Entity', 'service name'], ['Order', 'primary value']], hint: 'Map each row to a readable label and primary value.', shape: [['label', 'string'], ['primary_value', 'number'], ['status', 'optional']] },
  { type: 'span_list', icon: '⌗', label: 'Span list', description: 'Inspect spans', fields: [['Order', 'newest first'], ['Limit', '100 spans']], hint: 'Include trace_id to make each span navigable.', shape: [['span_id', 'string'], ['trace_id', 'string'], ['name', 'string']] },
  { type: 'trace_list', icon: '◌', label: 'Trace list', description: 'Investigate requests', fields: [['Order', 'newest first'], ['Limit', '100 traces']], hint: 'Include trace_id and a human-readable operation name.', shape: [['trace_id', 'string'], ['service_name', 'string'], ['name', 'string']] },
  { type: 'log_list', icon: '≡', label: 'Log list', description: 'Read events', fields: [['Order', 'newest first'], ['Limit', '100 logs']], hint: 'Include timestamp, severity, and body.', shape: [['timestamp', 'timestamp'], ['severity', 'string'], ['body', 'string']] },
  { type: 'deploy_correlation', icon: '↗', label: 'Deploy correlation', description: 'Annotate a series', fields: [['Measure', 'request latency'], ['Annotations', 'release events']], hint: 'Keep a series even when no release source is connected.', shape: [['timestamp', 'timestamp'], ['value', 'number'], ['annotation', 'optional']] },
] as const

function readLocalDashboards(): Dashboard[] {
  try { return JSON.parse(sessionStorage.getItem(localKey) ?? '[]') as Dashboard[] } catch { return [] }
}

function saveLocalDashboards(dashboards: Dashboard[]) {
  sessionStorage.setItem(localKey, JSON.stringify(dashboards))
}

// Vite's SPA fallback returns index.html for /api while the Go server is not
// running. That surfaces as a JSON SyntaxError, which is distinct from a real
// API persistence error and is safe to keep as a local draft.
function backendUnavailable(error: unknown) {
  return error instanceof TypeError || error instanceof SyntaxError
}

function nextDashboardName(dashboards: Dashboard[]) {
  const used = dashboards.reduce((highest, dashboard) => {
    const match = /^New dashboard (\d+)$/i.exec(dashboard.name)
    return Math.max(highest, match ? Number(match[1]) : 0)
  }, 0)
  return `New dashboard ${used + 1}`
}

function DashboardGallery() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { data: savedDashboards = [] } = useQuery({ queryKey: qk.dashboards(), queryFn: () => api.dashboards.list().then(x => x.data) })
  const [local, setLocal] = useState<Dashboard[]>(readLocalDashboards)
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedId = searchParams.get('id')
  const [reordering, setReordering] = useState(false)
  const reorderPending = useRef(false)
  const dashboards = [...savedDashboards, ...local.filter(localDashboard => !savedDashboards.some(saved => saved.id === localDashboard.id))]
  const selected = dashboards.find(dashboard => dashboard.id === selectedId) ?? dashboards[0]
  const [variables, setVariables] = useState<Record<string, string>>({})
  const [panelStatus, setPanelStatus] = useState<Record<string, { state: 'loading' | 'ready' | 'error'; message?: string }>>({})
  useEffect(() => { if (selected) setVariables(Object.fromEntries(selected.variables.map(variable => [variable.name, variable.default_value]))) }, [selected?.id])
  const dashboardHref = (id: string, index: number) => {
    const next = new URLSearchParams(searchParams)
    if (index === 0) next.delete('id'); else next.set('id', id)
    return `/dashboards${next.size ? `?${next}` : ''}`
  }
  const reorder = async (from: string, to: string) => {
    if (from === to || reorderPending.current) return
    const next = [...dashboards]
    const source = next.findIndex(item => item.id === from), target = next.findIndex(item => item.id === to)
    if (source < 0 || target < 0) return
    next.splice(target, 0, ...next.splice(source, 1))
    reorderPending.current = true
    setReordering(true)
    try {
      await api.dashboards.reorder(next.filter(item => !item.id.startsWith('local-')).map(item => item.id))
      // Preserve the viewed dashboard when moving a different entry to first.
      setSearchParams(current => { const params = new URLSearchParams(current); if (selected && selected.id !== next[0]?.id) params.set('id', selected.id); else params.delete('id'); return params }, { replace: true })
      qc.setQueryData(qk.dashboards(), next.filter(item => !item.id.startsWith('local-')))
      await qc.invalidateQueries({ queryKey: qk.dashboards() })
    } catch (error) {
      toast.error(`Could not reorder dashboards: ${error instanceof Error ? error.message : String(error)}`)
    } finally { reorderPending.current = false; setReordering(false) }
  }
  const syncLocal = (next: Dashboard[]) => { setLocal(next); saveLocalDashboards(next) }
  const remove = async (dashboard: Dashboard) => {
    if (!window.confirm(`Delete “${dashboard.name}”? This cannot be undone.`)) return
    if (dashboard.id.startsWith('local-')) syncLocal(local.filter(item => item.id !== dashboard.id))
    else { await api.dashboards.remove(dashboard.id); await qc.invalidateQueries({ queryKey: qk.dashboards() }) }
  }
  const rename = async (dashboard: Dashboard) => {
    const name = window.prompt('Dashboard name', dashboard.name)?.trim()
    if (!name || name === dashboard.name) return
    if (dashboard.id.startsWith('local-')) syncLocal(local.map(item => item.id === dashboard.id ? { ...item, name, updated_at: Date.now() * 1_000_000 } : item))
    else { await api.dashboards.update(dashboard.id, { name, description: dashboard.description }); await qc.invalidateQueries({ queryKey: qk.dashboards() }) }
  }
  return <div className="dashboard-workspace flex min-h-0 flex-1 overflow-hidden bg-background text-foreground">
    <aside className="w-56 shrink-0 overflow-auto border-r border-border bg-surface"><p className="px-4 pb-2 pt-5 font-mono text-[10px] uppercase tracking-[.12em] text-muted-foreground">Dashboards</p><DashboardList dashboards={dashboards} selectedId={selected?.id} href={dashboardHref} reorder={reorder} pending={reordering}/><Link to="/dashboards/new" className="mx-3 mt-3 flex cursor-pointer items-center justify-center gap-1 rounded-md border border-dashed border-accent px-2 py-2 text-xs font-semibold text-accent-ink"><Plus size={14}/> New dashboard</Link></aside>
    <main className="min-w-0 flex-1 overflow-auto"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div><h1 className="text-[22px] font-semibold tracking-tight">{selected?.name ?? 'Dashboards'}</h1><p className="mt-1 text-xs text-muted-foreground">{selected?.description || 'Query-backed telemetry views.'}</p></div><div className="ml-auto"><button disabled={selected?.id.startsWith('file-')} onClick={() => selected && navigate(`/dashboards/${selected.id}`)} className="cursor-pointer rounded-md bg-[#315b7d] px-3 py-2 text-xs font-medium text-white hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50">{selected?.id.startsWith('file-') ? 'File-managed' : 'Edit dashboard'}</button></div></header>{selected ? <div className="mx-auto max-w-6xl p-5">{Object.values(panelStatus).some(status => status.state === 'error') && <p role="alert" className="mb-4 rounded border border-danger/40 bg-danger/10 px-3 py-2 text-xs text-danger">Partial dashboard data: {Object.values(panelStatus).filter(status => status.state === 'error').length} panel query{Object.values(panelStatus).filter(status => status.state === 'error').length === 1 ? '' : 'ies'} failed. Healthy panels are still shown.</p>}{selected.id.startsWith('file-') && <p className="mb-4 rounded border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">File-managed dashboard: its YAML file is authoritative and reloads on startup. Export/import its YAML to create an editable local copy.</p>}<div className="mb-4 flex flex-wrap gap-2">{selected.variables.map(variable => <label key={variable.name} className="font-mono text-[11px] text-muted-foreground">${variable.name}<input aria-label={`Dashboard variable ${variable.name}`} value={variables[variable.name] ?? ''} onChange={event => setVariables(current => ({ ...current, [variable.name]: event.target.value }))} className="ml-1 rounded border border-border bg-surface px-2 py-1"/></label>)}</div><div className="grid auto-rows-[420px] gap-4 lg:grid-cols-12">{selected.panels.map(panel => <Panel key={panel.id} panel={panel} dashboardId={selected.id} variables={variables} onStatus={(id, state, message) => setPanelStatus(current => current[id]?.state === state && current[id]?.message === message ? current : { ...current, [id]: { state, message } })} refresh={() => qc.invalidateQueries({queryKey:qk.dashboards()})} onEdit={() => navigate(`/dashboards/${selected.id}`)}/>)}{selected.panels.length === 0 && <div className="rounded-lg border border-dashed border-border p-12 text-center text-sm text-muted-foreground">This dashboard has no panels yet. Select <b>Edit dashboard</b> to add one.</div>}</div></div> : <div className="p-12 text-center"><h2 className="text-lg font-semibold">No dashboards yet</h2><Link to="/dashboards/new" className="mt-4 inline-flex rounded bg-accent px-3 py-2 text-sm text-accent-ink">Create dashboard</Link></div>}</main>
  </div>
}


export default function Dashboards() {
  return <DashboardGallery />
}

function Panel({ panel, dashboardId, variables: sharedVariables, onStatus, moveUp, moveDown, moving }: { panel: DashboardPanel; dashboardId: string; variables?: Record<string, string>; onStatus?: (id: string, state: 'loading' | 'ready' | 'error', message?: string) => void; refresh?: () => void; onEdit?: (panel: DashboardPanel) => void; moveUp?: () => void; moveDown?: () => void; moving?: boolean }) {
  const { data: dashboard } = useQuery({ queryKey: ['dashboard-runtime', dashboardId], queryFn: () => api.dashboards.get(dashboardId).then(x => x.data) })
  const [variables, setVariables] = useState<Record<string, string>>({})
  useEffect(() => {
    if (!dashboard) return
    setVariables(Object.fromEntries(dashboard.variables.map(variable => [variable.name, variable.default_value])))
  }, [dashboard?.variables])
  let layout: { width?: string; x?: number; y?: number; w?: number; h?: number } = {}; try { layout = JSON.parse(panel.layout_json) as typeof layout } catch { /* default */ }
  const w = layout.w ?? (layout.width === 'wide' ? 12 : 6)
  return <article style={{ gridColumn: `span ${Math.max(1, Math.min(12, w))} / span ${Math.max(1, Math.min(12, w))}`, gridColumnStart: layout.x ? Math.max(1, Math.min(12, layout.x)) : undefined, gridRow: `span ${Math.max(1, Math.min(6, layout.h ?? 1))} / span ${Math.max(1, Math.min(6, layout.h ?? 1))}`, gridRowStart: layout.y ? Math.max(1, layout.y) : undefined }} className="min-h-[180px] overflow-hidden rounded-lg border border-border bg-surface">
<header className="border-b border-border px-4 py-3"><div className="flex items-center gap-2"><h2 className="min-w-0 flex-1 truncate text-[13px] font-semibold">{panel.title}</h2>{moveUp && <button type="button" aria-label={`Move ${panel.title} up`} title="Move panel up" disabled={moving} onClick={moveUp} className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-wait disabled:opacity-50"><ArrowUp size={16}/></button>}{moveDown && <button type="button" aria-label={`Move ${panel.title} down`} title="Move panel down" disabled={moving} onClick={moveDown} className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-wait disabled:opacity-50"><ArrowDown size={16}/></button>}</div><Tooltip><TooltipTrigger aria-label={`Show SQL for ${panel.title}`} className="mt-1 block w-full cursor-help truncate rounded text-left font-mono text-[10px] text-muted-foreground">{panel.display_type} · {panel.query_sql}</TooltipTrigger><TooltipContent side="bottom" align="start" className="max-w-[min(38rem,calc(100vw-2rem))] items-start border border-border bg-popover p-3 text-popover-foreground shadow-lg"><SqlCode value={panel.query_sql}/></TooltipContent></Tooltip></header>
    <div className="px-4 py-3">{sharedVariables ? null : dashboard?.variables.length ? <div className="mb-3 flex flex-wrap gap-2">{dashboard.variables.map(variable => <label key={variable.name} className="font-mono text-[10px] text-muted-foreground">${variable.name}<input aria-label={`Dashboard variable ${variable.name}`} value={variables[variable.name] ?? ''} onChange={event => setVariables(current => ({ ...current, [variable.name]: event.target.value }))} className="ml-1 rounded border border-border bg-background px-1 py-0.5 text-foreground"/></label>)}</div> : null}<PanelRenderer dashboardId={dashboardId} panel={panel} variables={sharedVariables ?? variables} onStatus={onStatus}/></div>
  </article>
}

function CatalogSql({ value }: { value: string }) {
  const tokens = value.split(/('(?:''|[^'])*'|\b(?:SELECT|FROM|WHERE|GROUP|BY|ORDER|LIMIT|AS|AND|OR|CASE|WHEN|THEN|ELSE|END|DESC|ASC|COUNT|AVG|SUM|MIN|MAX|CAST|DISTINCT)\b|\b\d+(?:\.\d+)?\b)/gi)
  return <code className="mt-1 block whitespace-pre-wrap break-words font-mono text-[10px] leading-4 text-muted-foreground">{tokens.map((token, index) => {
    if (/^'/.test(token)) return <span key={index} className="text-[var(--sql-string)]">{token}</span>
    if (/^\d/.test(token)) return <span key={index} className="text-[var(--sql-number)]">{token}</span>
    if (/^(select|from|where|group|by|order|limit|as|and|or|case|when|then|else|end|desc|asc|count|avg|sum|min|max|cast|distinct)$/i.test(token)) return <span key={index} className="font-semibold text-[var(--sql-keyword)]">{token}</span>
    return token
  })}</code>
}

function readSettings(value: string): Record<string, unknown> { try { return JSON.parse(value) as Record<string, unknown> } catch { return {} } }
function writeSettings(value: string, change: (settings: Record<string, unknown>) => void) { change(readSettings(value)) }

function RendererSettings({ display, value, onChange }: { display: string; value: string; onChange: (value: string) => void }) {
  const settings = readSettings(value)
  const update = (next: Record<string, unknown>) => onChange(JSON.stringify(next))
  const field = (label: string, key: string, nested = false) => <label className="font-mono text-[10px] text-muted-foreground">{label}<input value={String(nested ? (settings.columns as Record<string, string> | undefined)?.[key] ?? '' : settings[key] ?? '')} onChange={event => update(nested ? { ...settings, columns: { ...(settings.columns as Record<string, string> ?? {}), [key]: event.target.value } } : { ...settings, [key]: event.target.value })} className="mt-1 block w-full rounded border border-input bg-background px-2 py-1 font-sans text-[11px] text-foreground"/></label>
  if (display === 'deploy_correlation') return <div className="grid gap-2 sm:col-span-2 sm:grid-cols-2"><p className="sm:col-span-2 font-mono text-[10px] text-muted-foreground">Release annotations</p>{field('Release label', 'annotation_label')}{field('Annotation SQL', 'annotation_query')}</div>
  if (display === 'entity_list') return <div className="grid gap-2 sm:col-span-2 sm:grid-cols-3"><p className="sm:col-span-3 font-mono text-[10px] text-muted-foreground">Entity columns</p>{field('Label column', 'label', true)}{field('Value column', 'value', true)}{field('Secondary column', 'secondary', true)}{field('Badge column', 'badge', true)}{field('Link column', 'link', true)}{field('Unit', 'unit')}</div>
  if (display === 'time_series' || display === 'heatmap') return <div className="grid gap-2 sm:col-span-2 sm:grid-cols-2"><label className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground"><input type="checkbox" checked={Boolean(settings.scroll)} onChange={event => update({ ...settings, scroll: event.target.checked })}/> Scroll oversized content</label>{field('Maximum height (px)', 'max_height_px')}</div>
  return null
}

function YamlCode({ value }: { value: string }) {
  return <code>{value.split(/(#[^\n]*|^\s*[\w_]+:|"[^"]*"|'[^']*'|\b\d+\b)/gm).map((token, index) => {
    if (token.startsWith('#')) return <span key={index} className="text-muted-foreground">{token}</span>
    if (/^\s*[\w_]+:$/.test(token)) return <span key={index} className="font-semibold text-violet-700 dark:text-violet-300">{token}</span>
    if (/^['"]/.test(token)) return <span key={index} className="text-emerald-700 dark:text-emerald-300">{token}</span>
    if (/^\d+$/.test(token)) return <span key={index} className="text-amber-700 dark:text-amber-300">{token}</span>
    return token
  })}</code>
}

export function TelemetryBrowser({ search, setSearch, catalog, select, createPanel, loading, error }: { loading?: boolean; error?: string; search: string; setSearch: (value: string) => void; catalog: QueryCatalogEntry[]; select: (item: QueryCatalogEntry) => void; createPanel: (item: QueryCatalogEntry) => void }) {
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const choose = (item: QueryCatalogEntry) => { setSelectedKey(`${item.signal}:${item.name}:${item.display_type}:${item.query}`); select(item) }
  return <section><header className="border-b border-border py-3"><div className="flex items-center justify-between gap-3"><div><h2 className="text-sm font-semibold">Library of useful and synthetic panels</h2><p className="mt-1 text-xs text-muted-foreground">Search examples or telemetry collected by Spaniel—metrics, spans, traces, and logs—to build panels.</p></div><input aria-label="Search telemetry SQL examples" value={search} onChange={event => setSearch(event.target.value)} placeholder="Search examples and telemetry" className="w-44 rounded border border-input bg-background px-2 py-1.5 text-xs"/></div></header><div className="overflow-auto">{loading ? <p role="status" className="p-4 text-xs text-muted-foreground">Searching examples and telemetry…</p> : error ? <p role="alert" className="p-4 text-xs text-danger">Telemetry search failed: {error}</p> : catalog.slice(0, 256).map(item => { const key = `${item.signal}:${item.name}:${item.display_type}:${item.query}`; const selected = selectedKey === key; return <div key={`${key}:${item.query}`} role="button" tabIndex={0} onClick={() => choose(item)} aria-pressed={selected} onKeyDown={event => { if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); choose(item) } }} className={`cursor-pointer border-b border-border px-4 py-2.5 text-left ${selected ? 'bg-accent-bg ring-1 ring-inset ring-accent' : 'hover:bg-muted'}`}><span className="text-xs font-medium">{item.name}</span><span className="ml-2 font-mono text-[9px] text-muted-foreground">{item.display_type}</span><CatalogAttributes attributes={item.attributes}/><CatalogSql value={item.query}/>{selected ? <div className="mt-2 flex justify-end"><button type="button" onClick={event => { event.stopPropagation(); createPanel(item) }} className="cursor-pointer rounded bg-accent px-3 py-1.5 text-xs font-medium text-accent-ink">Create panel</button></div> : null}</div> })}{!loading && !error && catalog.length === 0 ? <p className="p-4 text-sm text-muted-foreground">No examples or observed telemetry match this search.</p> : null}</div></section>
}

function PanelStudio({ recipe, title, setTitle, display, setDisplay, query, setQuery, settingsJSON, setSettingsJSON, layout, setLayout, preview, previewState, save, editing }: { recipe: typeof panelRecipes[number]; title: string; setTitle: (value: string) => void; display: string; setDisplay: (value: string) => void; query: string; setQuery: (value: string) => void; settingsJSON: string; setSettingsJSON: (value: string) => void; layout: { x: number; y: number; w: number; h: number }; setLayout: (value: { x: number; y: number; w: number; h: number }) => void; preview: () => void; previewState: { data?: PreviewSnapshot; isPending: boolean; error: Error | null }; save: () => void; editing: boolean }) {
  return <section>
    <header className="border-b border-border py-3"><h2 className="text-[13px] font-semibold">{editing ? 'Edit panel' : 'Design a panel'}</h2><p className="mt-1 text-[11px] leading-[1.4] text-muted-foreground">Start from the question you want answered. Spaniel explains the result shape and offers editable sample SQL—it does not guess your telemetry.</p></header>
    <div className="grid grid-cols-4 gap-2 border-b border-border py-2.5">{panelRecipes.slice(0, 8).map(item => <button key={item.type} onClick={() => setDisplay(item.type)} className={`min-h-[68px] rounded-md border p-2 text-left text-[10px] ${display === item.type ? 'border-[#7aa3c4] bg-[#e5f0f7] text-accent-ink shadow-[inset_2px_0_0_var(--accent)] dark:bg-accent-bg' : 'border-[#d3e1ea] bg-surface text-muted-foreground hover:border-accent'}`}><span className="block text-[11px] font-semibold text-foreground"><i className="mr-1 font-mono text-sm not-italic text-accent-ink">{item.icon}</i>{item.label}</span><span className="mt-1 block leading-[1.25]">{item.description}</span></button>)}</div>
    <div className="grid gap-3 py-3 sm:grid-cols-[minmax(0,1.25fr)_minmax(230px,.75fr)]"><PanelBuilderControls key={display} display={display} apply={setQuery}/><aside className="rounded-md border border-border bg-background p-2.5"><h3 className="mb-2 text-[11px] font-semibold">Expected result</h3>{recipe.shape.map(([name, type]) => <div key={name} className="flex justify-between border-b border-border py-1.5 font-mono text-[10px] last:border-0"><span>{name}</span><span className="text-emerald-700 dark:text-emerald-300">{type}</span></div>)}<p className="mt-3 text-[10px] text-muted-foreground">{recipe.hint}</p></aside></div>
    <section className="border-t border-border bg-background"><div className="w-full border-b border-border bg-accent-bg px-3 py-2"><strong className="block whitespace-nowrap text-[11px]">Read-only DuckDB SQL</strong></div><div className="grid gap-2 border-b border-border bg-muted/30 p-2.5 sm:grid-cols-2"><label className="font-mono text-[10px] text-muted-foreground">Panel name<input value={title} onChange={event => setTitle(event.target.value)} placeholder="e.g. Checkout errors" className="mt-1 block w-full rounded border border-input bg-background px-2 py-1.5 font-sans text-[11px] text-foreground"/></label><fieldset className="font-mono text-[10px] text-muted-foreground"><legend>Canvas placement</legend><div className="mt-1 grid grid-cols-4 gap-1">{(['x', 'y', 'w', 'h'] as const).map(key => <label key={key}>{key}<input aria-label={`Panel ${key}`} type="number" min="1" max={key === 'w' ? 12 : key === 'h' ? 6 : 99} value={layout[key]} onChange={event => setLayout({ ...layout, [key]: Math.max(1, Number(event.target.value) || 1) })} className="mt-1 w-full rounded border border-input bg-background px-1 py-1 text-[11px]"/></label>)}</div></fieldset><RendererSettings display={display} value={settingsJSON} onChange={setSettingsJSON}/>{['deploy_correlation', 'entity_list', 'time_series', 'heatmap'].includes(display) ? <label className="sm:col-span-2 font-mono text-[10px] text-muted-foreground">Advanced renderer settings JSON<textarea aria-label="Renderer settings JSON" value={settingsJSON} onChange={event => setSettingsJSON(event.target.value)} className="mt-1 block min-h-20 w-full rounded border border-input bg-background p-2 font-mono text-[11px]"/></label> : null}</div><div className="p-2.5"><SqlEditor value={query} onChange={setQuery}/></div><div className="flex justify-end gap-2 border-t border-border px-2.5 py-2"><button type="button" onClick={preview} disabled={previewState.isPending || !query.trim()} className="cursor-pointer rounded border border-border bg-background px-3 py-1.5 text-[11px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">{previewState.isPending ? 'Running preview…' : 'Run preview'}</button><button type="button" onClick={save} className="cursor-pointer rounded bg-accent px-3 py-1.5 text-[11px] font-medium text-accent-ink">{editing ? 'Save changes' : 'Add panel to draft'}</button></div><PanelPreview title={title} query={query} display={display} result={previewState.data} pending={previewState.isPending} error={previewState.error}/></section>
  </section>
}

export function DashboardEditor() {
  const { dashboardId } = useParams()
  const editorNavigate = useNavigate()
  const qc = useQueryClient(); const refresh = () => qc.invalidateQueries({ queryKey: qk.dashboards() })
  const { data: savedDashboards = [] } = useQuery({ queryKey: qk.dashboards(), queryFn: () => api.dashboards.list().then(x => x.data) })
  const [localDashboards, setLocalDashboards] = useState<Dashboard[]>(readLocalDashboards)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedTab = searchParams.get('tab')
  const editorTab = requestedTab === 'library' || requestedTab === 'panels' ? requestedTab : 'design'
  const setEditorTab = (tab: string) => {
    if (tab !== 'design' && tab !== 'library' && tab !== 'panels') return
    setSearchParams(current => {
      const next = new URLSearchParams(current)
      next.set('tab', tab)
      return next
    })
  }
  const [templateId, setTemplateId] = useState('none')
  const [creating, setCreating] = useState(false)
  const creatingRef = useRef(false)
  const [createError, setCreateError] = useState('')
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  useEffect(() => { if (!dashboardId) { setTemplateId('none'); setCreateError(''); setImportError('') } }, [dashboardId])
  const [selected, setSelected] = useState<string | null>(dashboardId ?? null); const [name, setName] = useState(''); const [description, setDescription] = useState(''); const [dashboardName, setDashboardName] = useState(''); const [query, setQuery] = useState('SELECT (start_ns // 60000000000) * 60000000000 AS timestamp_ns, count(*) AS value FROM spans GROUP BY 1 ORDER BY 1'); const [title, setTitle] = useState('Span count'); const [display, setDisplay] = useState<string>('time_series'); const [settingsJSON, setSettingsJSON] = useState('{}'); const [layout, setLayout] = useState({ x: 1, y: 1, w: 6, h: 1 }); const [editing, setEditing] = useState<DashboardPanel | null>(null); const [variableName, setVariableName] = useState('service'); const [variableSource, setVariableSource] = useState('telemetry_spans.service_name'); const [variableKind, setVariableKind] = useState<DashboardVariable['kind']>('string'); const [variableDefault, setVariableDefault] = useState(''); const [catalogSearch, setCatalogSearch] = useState('')
  const [configText, setConfigText] = useState<string | null>(null); const [configError, setConfigError] = useState<string | null>(null)
  const dashboards = [...savedDashboards, ...localDashboards.filter(localDashboard => !savedDashboards.some(saved => saved.id === localDashboard.id))]
  // /dashboards/new is a real creation canvas, not an implicit edit of the
  // first saved dashboard.
  // The creation route must never inherit a dashboard selected earlier in this
  // mounted editor instance. Its URL is the source of truth.
  const active = useMemo(() => dashboardId ? dashboards.find(x => x.id === dashboardId) : undefined, [dashboards, dashboardId])
  const defaultName = nextDashboardName(dashboards)
  const [debouncedSearch, setDebouncedSearch] = useState(catalogSearch)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(catalogSearch.trim()), 250)
    return () => window.clearTimeout(timer)
  }, [catalogSearch])
  const activeSession = useQuery({ queryKey: qk.activeSession(), queryFn: () => api.sessions.getActive().then(response => response.data), enabled: Boolean(active), refetchInterval: 5000, staleTime: 5000 })
  const catalog = useQuery({ queryKey: ['query-catalog', debouncedSearch, activeSession.data?.id], queryFn: ({ signal }) => api.dashboards.catalog(undefined, debouncedSearch, signal).then(x => x.data), retry: 1, staleTime: 15_000 })
	const preview = useMutation({ mutationFn: () => active ? api.dashboards.preview(active.id, { query_sql: query, name: title, display_type: display }).then(x => ({ ...x.data, querySQL: query, displayType: display })) : Promise.reject(new Error('Create a dashboard before previewing SQL')) })
  useEffect(() => { setDashboardName(active?.name ?? '') }, [active?.id])
  const saveDashboardName = async (nextName: string) => {
    if (!active || !nextName.trim() || nextName.trim() === active.name) return
    const name = nextName.trim()
    if (active.id.startsWith('local-')) {
      setLocalDashboards(current => {
        const next = current.map(dashboard => dashboard.id === active.id ? { ...dashboard, name, updated_at: Date.now() * 1_000_000 } : dashboard)
        saveLocalDashboards(next)
        return next
      })
      return
    }
    await api.dashboards.update(active.id, { name, description: active.description })
    refresh()
  }
  const nameSave = useDebouncedSave({ key: active?.id ?? 'new-dashboard', value: dashboardName, enabled: Boolean(active), save: saveDashboardName })
  const create = async () => {
    if (creatingRef.current) return
    creatingRef.current = true
    setCreating(true); setCreateError('')
    const template = dashboardTemplates.find(item => item.id === templateId) ?? dashboardTemplates[0]
    const body = { name: name.trim() || (template.id === 'none' ? defaultName : template.name), description: description.trim() || template.description, panels: template.panels }
    try {
      const result = await api.dashboards.create(body)
      setSelected(result.data.id)
      qc.setQueryData<Dashboard[]>(qk.dashboards(), current => [...(current ?? []), result.data])
      void refresh()
      editorNavigate(`/dashboards/${result.data.id}`)
    } catch (error) {
      setCreateError(`Could not create dashboard: ${error instanceof Error ? error.message : String(error)}. Your selections are preserved.`)
    } finally {
      creatingRef.current = false
      setCreating(false)
    }
  }
  const importYAML = async (yaml: string) => {
    if (creatingRef.current || importing) return
    setImporting(true); setImportError('')
    try {
      const result = await api.dashboards.importConfig(yaml)
      setSelected(result.data.id)
      qc.setQueryData<Dashboard[]>(qk.dashboards(), current => [...(current ?? []), result.data])
      void refresh()
      editorNavigate(`/dashboards/${result.data.id}`)
    } catch (error) {
      setImportError(`Could not import dashboard: ${error instanceof Error ? error.message : String(error)}`)
    } finally {
      setImporting(false)
    }
  }
  const savePanel = async () => {
    if (!active) return
    try { JSON.parse(settingsJSON) } catch { toast.error('Panel settings must be valid JSON.'); return }
    const body = { title, display_type: display as DashboardPanel['display_type'], query_sql: query, position: editing?.position ?? active.panels.length, settings_json: settingsJSON, layout_json: JSON.stringify(layout) }
    const saveLocalPanel = () => {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = editing
        ? { ...editing, ...body, updated_at: now }
        : { id: `local-panel-${crypto.randomUUID()}`, dashboard_id: active.id, ...body, query_version: 1, updated_at: now }
      setLocalDashboards(current => { const next = current.map(dashboard => dashboard.id !== active.id ? dashboard : { ...dashboard, updated_at: now, panels: editing ? dashboard.panels.map(item => item.id === panel.id ? panel : item) : [...dashboard.panels, panel] }); saveLocalDashboards(next); return next })
    }
    if (active.id.startsWith('local-')) {
      saveLocalPanel()
      setEditing(null)
      return
    }
    try {
      if (editing) await api.dashboards.updatePanel(active.id, editing.id, body)
      else await api.dashboards.panel(active.id, body)
      refresh()
    } catch (error) {
      if (!backendUnavailable(error)) throw error
	  // A non-local dashboard never turns into a hidden browser-only draft.
	  toast.error(`Could not save panel: ${error instanceof Error ? error.message : String(error)}`)
	  return
    }
    setEditing(null)
  }
  const createCatalogPanel = async (item: QueryCatalogEntry) => {
    if (!active) return
    const body = { title: item.name, display_type: item.display_type as DashboardPanel['display_type'], query_sql: item.query, position: active.panels.length }
    setTitle(body.title); setQuery(body.query_sql); setDisplay(body.display_type)
    if (active.id.startsWith('local-')) {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = { id: `local-panel-${crypto.randomUUID()}`, dashboard_id: active.id, ...body, query_version: 1, settings_json: '{}', layout_json: '{}', updated_at: now }
      setLocalDashboards(current => { const next = current.map(dashboard => dashboard.id === active.id ? { ...dashboard, updated_at: now, panels: [...dashboard.panels, panel] } : dashboard); saveLocalDashboards(next); return next })
      return
    }
    try {
      await api.dashboards.panel(active.id, body)
      await refresh()
      toast.success(`Added “${item.name}” to this dashboard`)
    } catch (error) {
      toast.error(`Could not create panel: ${error instanceof Error ? error.message : String(error)}`)
    }
  }
  const edit = (p: DashboardPanel) => { setEditorTab('design'); setEditing(p); setTitle(p.title); setQuery(p.query_sql); setDisplay(p.display_type); setSettingsJSON(p.settings_json || '{}'); try { const saved = JSON.parse(p.layout_json) as { x?: number; y?: number; w?: number; h?: number; width?: string }; setLayout({ x: saved.x ?? 1, y: saved.y ?? 1, w: saved.w ?? (saved.width === 'wide' ? 12 : 6), h: saved.h ?? 1 }) } catch { setLayout({ x: 1, y: 1, w: 6, h: 1 }) } }
  const [movingPanel, setMovingPanel] = useState(false)
  const movePending = useRef(false)
  const movePanel = async (panel: DashboardPanel, direction: -1 | 1) => {
    if (!active || movePending.current) return
    movePending.current = true
    setMovingPanel(true)
    try {
      if (active.id.startsWith('local-')) {
        setLocalDashboards(current => {
          const next = current.map(dashboard => {
            if (dashboard.id !== active.id) return dashboard
            const panels = [...dashboard.panels]
            const index = panels.findIndex(item => item.id === panel.id)
            const target = index + direction
            if (index < 0 || target < 0 || target >= panels.length) return dashboard
            ;[panels[index], panels[target]] = [panels[target], panels[index]]
            return { ...dashboard, panels: panels.map((item, position) => ({ ...item, position })) }
          })
          saveLocalDashboards(next)
          return next
        })
      } else {
        await api.dashboards.movePanel(active.id, panel.id, direction)
        await refresh()
      }
    } catch (error) {
      toast.error(`Could not move panel: ${error instanceof Error ? error.message : String(error)}`)
    } finally {
      movePending.current = false
      setMovingPanel(false)
    }
  }
  const saveCanvasLayout = async (panel: DashboardPanel, nextLayout: PanelLayout) => {
    if (!active) return
    const body = { title: panel.title, display_type: panel.display_type, query_sql: panel.query_sql, position: panel.position, settings_json: panel.settings_json, layout_json: JSON.stringify(nextLayout) }
    setMovingPanel(true)
    try {
      if (active.id.startsWith('local-')) {
        setLocalDashboards(current => {
          const next = current.map(dashboard => dashboard.id !== active.id ? dashboard : { ...dashboard, panels: dashboard.panels.map(item => item.id === panel.id ? { ...item, layout_json: body.layout_json, updated_at: Date.now() * 1_000_000 } : item) })
          saveLocalDashboards(next)
          return next
        })
      } else {
        await api.dashboards.updatePanel(active.id, panel.id, body)
        await refresh()
      }
    } catch (error) { toast.error(`Could not save panel layout: ${error instanceof Error ? error.message : String(error)}`) } finally { setMovingPanel(false) }
  }
  const addVariable = async () => {
    if (!active || !variableName.trim() || !variableSource.trim()) return
    const body = { name: variableName.trim(), kind: variableKind, source: variableSource.trim(), default_value: variableDefault.trim(), options_json: variableKind === 'enum' ? JSON.stringify(variableSource.split(',').map(value => value.trim()).filter(Boolean)) : '[]' }
    try {
      await api.dashboards.variable(active.id, body)
      refresh()
    } catch (error) {
      if (!active.id.startsWith('local-')) {
        toast.error(`Could not save variable: ${error instanceof Error ? error.message : String(error)}`)
        return
      }
      setLocalDashboards(current => {
        const next = current.map(dashboard => dashboard.id !== active.id ? dashboard : { ...dashboard, variables: [...dashboard.variables.filter(item => item.name !== body.name), { dashboard_id: active.id, ...body }] })
        saveLocalDashboards(next)
        return next
      })
    }
  }
  const removeVariable = async (name: string) => {
    if (!active) return
    if (active.id.startsWith('local-')) {
      setLocalDashboards(current => {
        const next = current.map(d => d.id === active.id ? { ...d, variables: d.variables.filter(v => v.name !== name) } : d)
        saveLocalDashboards(next)
        return next
      })
    } else {
      await api.dashboards.deleteVariable(active.id, name)
      await refresh()
      await qc.invalidateQueries({ queryKey: ['dashboard-runtime', active.id] })
      await qc.invalidateQueries({ queryKey: ['dashboard-panel', active.id] })
    }
  }
  const deleteActive = async () => {
    if (!active || !window.confirm(`Delete “${active.name}”? This cannot be undone.`)) return
    try {
      await nameSave.flush()
      if (active.id.startsWith('local-')) {
        setLocalDashboards(current => { const next = current.filter(dashboard => dashboard.id !== active.id); saveLocalDashboards(next); return next })
      } else {
        await api.dashboards.remove(active.id)
        await refresh()
      }
      editorNavigate('/dashboards')
    } catch (error) {
      toast.error(`Could not delete dashboard: ${error instanceof Error ? error.message : String(error)}`)
    }
  }
  const viewTextConfig = async () => {
    if (!active || active.id.startsWith('local-')) { setConfigError('Save this dashboard before exporting its YAML configuration.'); setConfigText(''); return }
    try { setConfigError(null); setConfigText(await api.dashboards.config(active.id)) } catch (error) { setConfigError(error instanceof Error ? error.message : String(error)); setConfigText('') }
  }
  if (Boolean(active)) {
    const dashboard = active!
    return <div className="flex min-h-0 flex-1 overflow-hidden">
    <aside className="w-64 shrink-0 overflow-auto border-r border-border bg-surface"><div className="border-b border-border p-3"><div className="flex items-center gap-2 font-semibold"><LayoutDashboard size={15}/> Dashboards</div><Link to="/dashboards/new" className="mt-3 flex w-full items-center justify-center gap-1 rounded border border-dashed border-accent bg-accent-bg px-2 py-2 text-xs font-medium text-accent-ink"><Plus size={14}/> New dashboard</Link></div>{dashboards.map(d => <Link key={d.id} to={`/dashboards/${d.id}`} className={`block border-b border-border px-3 py-3 ${dashboard.id === d.id ? 'bg-accent-bg' : 'hover:bg-muted'}`}><div className="truncate text-sm font-medium">{d.name}</div><div className="mt-1 font-mono text-[10px] text-muted-foreground">{d.panels.length} panels</div></Link>)}</aside>
    <main className="flex-1 overflow-auto bg-background"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div className="min-w-0 flex-1"><div className="flex max-w-xl items-center gap-1"><input ref={titleInputRef} aria-label="Dashboard name" value={dashboardName} onChange={event => setDashboardName(event.target.value)} className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none"/><button type="button" aria-label="Edit dashboard name" onClick={() => titleInputRef.current?.focus()} className="rounded p-1.5 text-muted-foreground hover:bg-muted"><Pencil size={15}/></button></div><p className="mt-1 text-sm text-muted-foreground">{nameSave.pending ? 'Saving dashboard name…' : dashboard.description || 'Query-backed telemetry views.'}</p></div><button onClick={() => void viewTextConfig()} className="rounded border border-border px-3 py-2 text-xs">View YAML</button><button onClick={() => void deleteActive()} className="rounded border border-danger px-3 py-2 text-xs text-danger">Delete dashboard</button></header><div className="mx-auto max-w-5xl space-y-4 p-6">
      <Tabs value={editorTab} onValueChange={value => setEditorTab(String(value))}>
        <TabsList variant="line" aria-label="Panel editor" className="w-full justify-start border-b border-border">
          <TabsTrigger value="design" className="flex-none cursor-pointer px-4">Design</TabsTrigger>
          <TabsTrigger value="library" className="flex-none cursor-pointer px-4">Library</TabsTrigger>
          <TabsTrigger value="panels" className="flex-none cursor-pointer px-4">Panels</TabsTrigger>
        </TabsList>
        <TabsContent value="design" keepMounted>
      <PanelStudio recipe={panelRecipes.find(item => item.type === display) ?? panelRecipes[0]} title={title} setTitle={setTitle} display={display} setDisplay={setDisplay} query={query} setQuery={setQuery} settingsJSON={settingsJSON} setSettingsJSON={setSettingsJSON} layout={layout} setLayout={setLayout} preview={() => preview.mutate()} previewState={preview} save={savePanel} editing={Boolean(editing)}/>
        </TabsContent>
        <TabsContent value="library" keepMounted>
      <TelemetryBrowser loading={catalog.isPending || debouncedSearch !== catalogSearch.trim()} error={catalog.error?.message} search={catalogSearch} setSearch={setCatalogSearch} catalog={catalog.data ?? []} select={item => { setTitle(item.name); setQuery(item.query); setDisplay(item.display_type) }} createPanel={createCatalogPanel}/>
        </TabsContent>
        <TabsContent value="panels">
      <section className="pt-3"><DashboardCanvas panels={dashboard.panels} onEdit={edit} onCommit={saveCanvasLayout} onMove={movePanel} saving={movingPanel}/></section>
        </TabsContent>
      </Tabs>
    </div></main>{configText !== null && <div role="dialog" aria-modal="true" aria-label="Dashboard YAML configuration" className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"><section className="w-full max-w-3xl overflow-hidden rounded-lg border border-border bg-background shadow-xl"><header className="flex items-center justify-between border-b border-border px-4 py-3"><div><h2 className="font-semibold">Dashboard YAML</h2><p className="text-xs text-muted-foreground">Portable YAML definition; telemetry data is never included.</p></div><button onClick={() => setConfigText(null)} className="rounded border border-border px-2 py-1 text-xs">Close</button></header><pre className="max-h-[70vh] overflow-auto p-4 font-mono text-xs leading-5"><YamlCode value={configError || configText}/></pre></section></div>}
    <aside className="w-[320px] shrink-0 overflow-auto border-l border-border bg-surface p-4"><MagicParameters insert={value => setQuery(current => current + value)}/><section className="mt-4 rounded-lg border border-border bg-background p-3"><h2 className="flex items-center gap-2 text-sm font-semibold"><Variable size={14}/> Reusable parameters</h2><p className="mt-1 text-xs leading-4 text-muted-foreground">New dashboards include service, operation, status_code (0: unset), and severity (9: INFO). Set service and operation values before using them.</p><div className="mt-3 grid gap-2"><input aria-label="Variable name" value={variableName} onChange={event => setVariableName(event.target.value)} placeholder="service" className="w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/><div className="grid grid-cols-2 gap-2"><select aria-label="Variable datatype" value={variableKind} onChange={event => setVariableKind(event.target.value as DashboardVariable['kind'])} className="rounded border border-input bg-background px-2 py-1.5 text-xs"><option value="string">string</option><option value="number">number</option><option value="boolean">boolean</option><option value="duration">duration</option><option value="time">time range</option><option value="enum">enum</option></select><input aria-label="Variable default value" value={variableDefault} onChange={event => setVariableDefault(event.target.value)} placeholder="Default" className="min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs"/></div><input aria-label="Variable value source" value={variableSource} onChange={event => setVariableSource(event.target.value)} placeholder="telemetry_spans.service_name" className="w-full rounded border border-input bg-background px-2 py-1.5 font-mono text-xs"/><button onClick={addVariable} disabled={!variableName.trim() || !variableSource.trim()} className="rounded border border-border px-2 py-1.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">Add parameter</button></div><ReusableParameterList variables={dashboard.variables} insert={value => setQuery(current => current + value)} remove={removeVariable}/></section><section className="mt-4 rounded-lg border border-border bg-background p-3"><h2 className="text-sm font-semibold">Draft canvas</h2><p className="mt-1 text-xs text-muted-foreground">{dashboard.panels.length} query-backed panel{dashboard.panels.length === 1 ? '' : 's'}</p><div className="mt-3 space-y-2">{dashboard.panels.map(panel => <DraftPanelEntry key={panel.id} panel={panel} edit={edit}/>)}</div></section></aside>
  </div>}
  return <div className="flex min-h-0 flex-1 overflow-hidden">
    <aside className="w-64 shrink-0 overflow-auto border-r border-border bg-surface"><div className="border-b border-border p-3"><div className="flex items-center gap-2 font-semibold"><LayoutDashboard size={15}/> Dashboards</div><Link to="/dashboards/new" className="mt-3 flex w-full cursor-pointer items-center justify-center gap-1 rounded border border-dashed border-accent bg-accent-bg px-2 py-2 text-xs font-medium text-accent-ink"><Plus size={14}/> New dashboard</Link></div>{dashboards.map(d => <Link key={d.id} to={`/dashboards/${d.id}`} className={`block w-full cursor-pointer border-b border-border px-3 py-3 text-left ${active?.id === d.id ? 'bg-accent-bg' : 'hover:bg-muted'}`}><div className="truncate text-sm font-medium">{d.name}</div><div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{d.panels.length} panels</div></Link>)}</aside>
    <main className="flex-1 overflow-auto bg-background"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div className="min-w-0 flex-1"><div className="flex max-w-xl items-center gap-1"><input ref={titleInputRef} aria-label="Dashboard name" disabled={!active} value={active ? dashboardName : 'New dashboard'} onChange={event => setDashboardName(event.target.value)} className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none placeholder:text-muted-foreground disabled:cursor-default"/><button type="button" aria-label="Edit dashboard name" title="Edit dashboard name" disabled={!active} onClick={() => titleInputRef.current?.focus()} className="cursor-pointer rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-40"><Pencil size={15}/></button></div><p className="mt-1 text-sm text-muted-foreground">{nameSave.pending ? 'Saving dashboard name…' : active?.description || 'DuckDB SQL telemetry views, kept locally with your traces.'}</p></div><button onClick={() => void deleteActive()} disabled={!active} className="cursor-pointer rounded border border-danger px-3 py-2 text-xs text-danger disabled:opacity-40">Delete dashboard</button></header>{!active ? <NewDashboardStarter name={name} setName={setName} description={description} setDescription={setDescription} create={() => void create()} templateId={templateId} setTemplateId={setTemplateId} pending={creating} error={createError} importYAML={yaml => void importYAML(yaml)} importing={importing} importError={importError}/> : <><section className="mx-6 mt-6 rounded-lg border border-border bg-surface"><div className="border-b border-border px-4 py-3"><h2 className="font-semibold">{editing ? 'Edit panel' : 'Design a panel'}</h2><p className="mt-1 text-xs text-muted-foreground">Choose a display shape first, then write the read-only query that produces it.</p></div><div className="grid gap-3 p-4 sm:grid-cols-2"><label className="block text-xs font-medium">Panel title<input value={title} onChange={e=>setTitle(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm"/></label><label className="block text-xs font-medium">Display<select value={display} onChange={e=>setDisplay(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm">{displays.map(x=><option key={x}>{x}</option>)}</select></label><label className="block text-xs font-medium sm:col-span-2">Read-only DuckDB SQL<SqlEditor value={query} onChange={setQuery}/></label><div className="sm:col-span-2 flex flex-wrap justify-end gap-2"><button onClick={() => preview.mutate()} disabled={preview.isPending} className="cursor-pointer rounded border border-border px-3 py-2 text-sm">{preview.isPending ? 'Running preview…' : 'Run preview'}</button><button onClick={savePanel} className="cursor-pointer rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink">{editing ? 'Save changes' : 'Add panel'}</button></div>{preview.isError&&<p role="alert" className="sm:col-span-2 text-sm text-danger">Preview failed: {preview.error.message}</p>}{preview.data&&<div className="sm:col-span-2 rounded border border-border bg-background p-3"><p className="font-mono text-[10px] text-muted-foreground">PREVIEW · {preview.data.rows.length} rows · {preview.data.columns.join(', ')}</p><pre className="mt-2 max-h-36 overflow-auto text-[11px]">{JSON.stringify(preview.data.rows.slice(0, 5), null, 2)}</pre></div>}</div></section><div className="grid gap-4 p-6 xl:grid-cols-2">{active.panels.map(p => <Panel key={p.id} panel={p} dashboardId={active.id} refresh={refresh} onEdit={edit}/>)}{active.panels.length === 0 && <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">Use Design a panel to add the first panel.</div>}</div></>}</main>
    <aside className="w-[360px] shrink-0 overflow-auto border-l border-border bg-surface p-4"><section><h2 className="text-sm font-semibold">Telemetry SQL</h2><p className="mt-1 text-xs text-muted-foreground">Queries run read-only against the stable telemetry views. Use <code>$name</code> for a dashboard parameter.</p><input aria-label="Search telemetry SQL examples" value={catalogSearch} onChange={e => setCatalogSearch(e.target.value)} placeholder="Search query examples" className="mt-3 w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/>{catalog.data?.map(item => <button key={`${item.signal}-${item.name}`} onClick={() => { setTitle(item.name); setQuery(item.query); setDisplay(item.display_type) }} className="mt-2 w-full rounded border border-border bg-background p-2 text-left hover:bg-muted"><span className="block text-xs font-medium">{item.name}</span><code className="mt-1 block truncate text-[10px] text-muted-foreground">{item.query}</code></button>)}</section><section className="mt-6 border-t border-border pt-4"><h2 className="flex items-center gap-2 text-sm font-semibold"><Variable size={14}/> Dashboard parameters</h2><p className="mt-1 text-xs text-muted-foreground">Start with an observed-telemetry field, then reference it as <code>$name</code> in DuckDB SQL.</p><div className="mt-3 flex flex-wrap gap-1">{variablePresets.map(preset => <button key={preset.name} onClick={() => { setVariableName(preset.name); setVariableKind(preset.kind); setVariableSource(preset.source); setVariableDefault(preset.default_value) }} className="cursor-pointer rounded border border-border bg-background px-2 py-1 font-mono text-[10px] hover:bg-muted">${preset.name}</button>)}</div><div className="mt-3 grid gap-2"><input aria-label="Variable name" value={variableName} onChange={e=>setVariableName(e.target.value)} placeholder="service" className="w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/><div className="grid grid-cols-2 gap-2"><select aria-label="Variable datatype" value={variableKind} onChange={e=>setVariableKind(e.target.value as DashboardVariable['kind'])} className="rounded border border-input bg-background px-2 py-1.5 text-xs"><option value="string">string</option><option value="number">number</option><option value="boolean">boolean</option><option value="duration">duration</option><option value="time">time range</option><option value="enum">enum</option></select><input aria-label="Variable default value" value={variableDefault} onChange={e=>setVariableDefault(e.target.value)} placeholder="Default value" className="min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs"/></div><input aria-label="Variable value source" value={variableSource} onChange={e=>setVariableSource(e.target.value)} placeholder={variableKind === 'enum' ? 'info, warn, error' : 'telemetry_spans.service_name'} className="w-full rounded border border-input bg-background px-2 py-1.5 font-mono text-xs"/><button onClick={addVariable} disabled={!active || !variableName.trim() || !variableSource.trim()} className="cursor-pointer rounded border border-border px-2 py-1.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">Add parameter</button></div>{active?.variables.map(v=><button key={v.name} onClick={() => setQuery(current => `${current}$${v.name}`)} className="mt-2 grid w-full grid-cols-[90px_1fr_60px] gap-2 truncate border-b border-border px-2 py-2 text-left font-mono text-[10px] hover:bg-muted" title={`Insert $${v.name}`}><span>${v.name}</span><span className="truncate">{v.source}</span><span>{v.kind}</span></button>)}</section>
      <section className="mt-6 border-t border-border pt-4"><h2 className="text-sm font-semibold">Draft canvas</h2><p className="mt-1 text-xs text-muted-foreground">{active ? `${active.panels.length} SQL panel${active.panels.length === 1 ? '' : 's'}` : 'Create a dashboard to begin.'}</p><div className="mt-3 space-y-2">{active?.panels.map(panel => <DraftPanelEntry key={panel.id} panel={panel} edit={edit}/>)}{active?.panels.length === 0 && <div className="rounded border border-dashed border-border p-4 text-xs text-muted-foreground">Panels you add appear here.</div>}</div></section></aside>
  </div>
}
