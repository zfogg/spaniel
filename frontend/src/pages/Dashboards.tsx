import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LayoutDashboard, Plus, Trash2, Variable, Pencil } from 'lucide-react'
import { api, type Dashboard, type DashboardPanel, type DashboardVariable } from '@/lib/api'
import { qk } from '@/lib/query'
import { useDebouncedSave } from '@/lib/useDebouncedSave'
import { toast } from 'sonner'
import { PanelRenderer } from '@/components/dashboard-panels/PanelRenderer'
import { SqlCode, SqlEditor } from '@/components/SqlCode'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const displays = ['single_value', 'time_series', 'table', 'heatmap', 'trace_list', 'log_list'] as const
const localKey = 'spaniel.local-dashboards'

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
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const dashboards = [...savedDashboards, ...local.filter(localDashboard => !savedDashboards.some(saved => saved.id === localDashboard.id))]
  const selected = dashboards.find(dashboard => dashboard.id === selectedId) ?? dashboards[0]
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
    <aside className="w-56 shrink-0 overflow-auto border-r border-border bg-surface"><p className="px-4 pb-2 pt-5 font-mono text-[10px] uppercase tracking-[.12em] text-muted-foreground">Dashboards</p>{dashboards.map(dashboard => <button key={dashboard.id} onClick={() => setSelectedId(dashboard.id)} className={`flex w-full cursor-pointer items-center gap-2 border-l-2 px-4 py-2 text-left text-sm ${selected?.id === dashboard.id ? 'border-accent bg-accent-bg font-semibold text-accent-ink' : 'border-transparent text-muted-foreground hover:bg-muted'}`}><LayoutDashboard size={14}/><span className="min-w-0 flex-1 truncate">{dashboard.name}</span><small className="font-mono text-[10px] text-muted-foreground">{dashboard.panels.length}</small></button>)}<Link to="/dashboards/new" className="mx-3 mt-3 flex cursor-pointer items-center justify-center gap-1 rounded-md border border-dashed border-accent px-2 py-2 text-xs font-semibold text-accent-ink"><Plus size={14}/> New dashboard</Link></aside>
    <main className="min-w-0 flex-1 overflow-auto"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div><h1 className="text-[22px] font-semibold tracking-tight">{selected?.name ?? 'Dashboards'}</h1><p className="mt-1 text-xs text-muted-foreground">{selected?.description || 'Query-backed telemetry views.'}</p></div><div className="ml-auto"><button onClick={() => selected && navigate(`/dashboards/${selected.id}`)} className="cursor-pointer rounded-md bg-[#315b7d] px-3 py-2 text-xs font-medium text-white hover:brightness-110">Edit dashboard</button></div></header>{selected ? <div className="mx-auto max-w-6xl p-5"><div className="mb-4 flex flex-wrap gap-2">{['service = all','environment = development','time Last 30 min ▾','+ add variable'].map(filter => <button key={filter} className="cursor-pointer rounded border border-border bg-surface px-2 py-1.5 font-mono text-[11px] text-muted-foreground">{filter}</button>)}</div><div className="grid gap-4 lg:grid-cols-2">{selected.panels.map(panel => <Panel key={panel.id} panel={panel} dashboardId={selected.id} refresh={() => qc.invalidateQueries({queryKey:qk.dashboards()})} onEdit={() => navigate(`/dashboards/${selected.id}`)}/>)}{selected.panels.length === 0 && <div className="rounded-lg border border-dashed border-border bg-surface p-12 text-center text-sm text-muted-foreground">This dashboard has no panels yet. Select <b>Edit dashboard</b> to add one.</div>}</div></div> : <div className="p-12 text-center"><h2 className="text-lg font-semibold">No dashboards yet</h2><Link to="/dashboards/new" className="mt-4 inline-flex rounded bg-accent px-3 py-2 text-sm text-accent-ink">Create dashboard</Link></div>}</main>
  </div>
}


export default function Dashboards() {
  return <DashboardGallery />
}

function Panel({ panel, dashboardId, refresh, onEdit, onMove, canMoveUp, canMoveDown }: { panel: DashboardPanel; dashboardId: string; refresh: () => void; onEdit: (p: DashboardPanel) => void; onMove?: (direction: -1 | 1) => void; canMoveUp?: boolean; canMoveDown?: boolean }) {
  const remove = async () => { await api.dashboards.removePanel(dashboardId, panel.id); refresh() }
  const { data: dashboard } = useQuery({ queryKey: ['dashboard-runtime', dashboardId], queryFn: () => api.dashboards.get(dashboardId).then(x => x.data) })
  const [variables, setVariables] = useState<Record<string, string>>({})
  useEffect(() => {
    if (!dashboard) return
    setVariables(Object.fromEntries(dashboard.variables.map(variable => [variable.name, variable.default_value])))
  }, [dashboard?.updated_at])
  return <article className="min-h-[180px] rounded-lg border border-border bg-surface shadow-sm overflow-hidden">
    <header className="flex items-start gap-2 border-b border-border px-4 py-3"><div className="min-w-0 flex-1"><h2 className="truncate font-semibold text-[13px]">{panel.title}</h2><Tooltip><TooltipTrigger asChild><p className="mt-1 cursor-help font-mono text-[10px] text-muted-foreground truncate">{panel.display_type} · {panel.query_sql}</p></TooltipTrigger><TooltipContent side="bottom" align="start" className="max-w-[min(38rem,calc(100vw-2rem))] items-start border border-border bg-popover p-3 text-popover-foreground shadow-lg"><SqlCode value={panel.query_sql}/></TooltipContent></Tooltip></div>{onMove&&<span className="flex gap-1"><button aria-label={`Move ${panel.title} up`} disabled={!canMoveUp} onClick={() => onMove(-1)} className="cursor-pointer px-1 text-muted-foreground disabled:opacity-30">↑</button><button aria-label={`Move ${panel.title} down`} disabled={!canMoveDown} onClick={() => onMove(1)} className="cursor-pointer px-1 text-muted-foreground disabled:opacity-30">↓</button></span>}<button aria-label={`Delete ${panel.title}`} onClick={remove} className="cursor-pointer text-muted-foreground hover:text-danger"><Trash2 size={14}/></button></header>
    <div className="p-4"><div className="mb-3 flex justify-end"><button onClick={() => onEdit(panel)} className="cursor-pointer rounded border border-border px-2 py-1 font-mono text-[10px] hover:bg-muted">Edit</button></div>{dashboard?.variables.length ? <div className="mb-3 flex flex-wrap gap-2">{dashboard.variables.map(variable => <label key={variable.name} className="font-mono text-[10px] text-muted-foreground">${variable.name}<input aria-label={`Dashboard variable ${variable.name}`} value={variables[variable.name] ?? ''} onChange={event => setVariables(current => ({ ...current, [variable.name]: event.target.value }))} className="ml-1 rounded border border-border bg-background px-1 py-0.5 text-foreground"/></label>)}</div> : null}<PanelRenderer dashboardId={dashboardId} panel={panel} variables={variables}/></div>
  </article>
}

export function DashboardEditor() {
  const { dashboardId } = useParams()
  const editorNavigate = useNavigate()
  const qc = useQueryClient(); const refresh = () => qc.invalidateQueries({ queryKey: qk.dashboards() })
  const { data: savedDashboards = [] } = useQuery({ queryKey: qk.dashboards(), queryFn: () => api.dashboards.list().then(x => x.data) })
  const [localDashboards, setLocalDashboards] = useState<Dashboard[]>(readLocalDashboards)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const [selected, setSelected] = useState<string | null>(dashboardId ?? null); const [name, setName] = useState(''); const [description, setDescription] = useState(''); const [dashboardName, setDashboardName] = useState(''); const [query, setQuery] = useState('SELECT date_trunc(\'minute\', make_timestamp_ns(start_ns)) AS timestamp, count(*) AS value FROM telemetry_spans GROUP BY 1 ORDER BY 1'); const [title, setTitle] = useState('Span count'); const [display, setDisplay] = useState<string>('time_series'); const [editing, setEditing] = useState<DashboardPanel | null>(null); const [variableName, setVariableName] = useState('customer_tier'); const [variableSource, setVariableSource] = useState('attributes.user.plan'); const [variableKind, setVariableKind] = useState<DashboardVariable['kind']>('string'); const [variableDefault, setVariableDefault] = useState(''); const [catalogSearch, setCatalogSearch] = useState('')
  const dashboards = [...savedDashboards, ...localDashboards.filter(localDashboard => !savedDashboards.some(saved => saved.id === localDashboard.id))]
  const active = useMemo(() => dashboards.find(x => x.id === (dashboardId ?? selected)) ?? (dashboardId ? undefined : dashboards[0]), [dashboards, dashboardId, selected])
  const defaultName = nextDashboardName(dashboards)
  const catalog = useQuery({ queryKey: ['query-catalog', catalogSearch], queryFn: () => api.dashboards.catalog(undefined, catalogSearch).then(x => x.data), enabled: Boolean(active) })
  const preview = useMutation({ mutationFn: () => active ? api.dashboards.preview(active.id, { query_sql: query, display_type: display }).then(x => x.data) : Promise.reject(new Error('Create a dashboard before previewing SQL')) })
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
    try {
      const result = await api.dashboards.create({ name: name.trim() || defaultName, description })
      setSelected(result.data.id)
      refresh()
      editorNavigate(`/dashboards/${result.data.id}`)
    } catch (error) {
      if (!backendUnavailable(error)) throw error
      const now = Date.now() * 1_000_000
      const draft: Dashboard = { id: `local-${crypto.randomUUID()}`, name: name.trim() || defaultName, description, created_at: now, updated_at: now, variables: [], panels: [] }
      setLocalDashboards(current => { const next = [...current, draft]; saveLocalDashboards(next); return next })
      setSelected(draft.id)
      editorNavigate(`/dashboards/${draft.id}`)
    }
  }
  const savePanel = async () => {
    if (!active) return
    const body = { title, display_type: display as DashboardPanel['display_type'], query_sql: query, position: editing?.position ?? active.panels.length }
    const saveLocalPanel = () => {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = editing
        ? { ...editing, ...body, updated_at: now }
        : { id: `local-panel-${crypto.randomUUID()}`, dashboard_id: active.id, ...body, query_version: 1, settings_json: '{}', layout_json: '{}', updated_at: now }
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
  const edit = (p: DashboardPanel) => { setEditing(p); setTitle(p.title); setQuery(p.query_sql); setDisplay(p.display_type) }
  const movePanel = async (panel: DashboardPanel, direction: -1 | 1) => {
    if (!active || active.id.startsWith('local-')) return
    const panels = [...active.panels].sort((a, b) => a.position - b.position)
    const index = panels.findIndex(item => item.id === panel.id); const other = panels[index + direction]
    if (!other) return
    await Promise.all([panel, other].map((item, i) => api.dashboards.updatePanel(active.id, item.id, { title: item.title, display_type: item.display_type, query_sql: item.query_sql, settings_json: item.settings_json, layout_json: item.layout_json, position: i === 0 ? other.position : panel.position })))
    refresh()
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
  return <div className="flex min-h-0 flex-1 overflow-hidden">
    <aside className="w-64 shrink-0 overflow-auto border-r border-border bg-surface"><div className="border-b border-border p-3"><div className="flex items-center gap-2 font-semibold"><LayoutDashboard size={15}/> Dashboards</div><button onClick={create} className="mt-3 flex w-full cursor-pointer items-center justify-center gap-1 rounded border border-dashed border-accent bg-accent-bg px-2 py-2 text-xs font-medium text-accent-ink"><Plus size={14}/> New dashboard</button></div>{dashboards.map(d => <Link key={d.id} to={`/dashboards/${d.id}`} className={`block w-full cursor-pointer border-b border-border px-3 py-3 text-left ${active?.id === d.id ? 'bg-accent-bg' : 'hover:bg-muted'}`}><div className="truncate text-sm font-medium">{d.name}</div><div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{d.panels.length} panels</div></Link>)}</aside>
    <main className="flex-1 overflow-auto bg-background"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div className="min-w-0 flex-1"><div className="flex max-w-xl items-center gap-1"><input ref={titleInputRef} aria-label="Dashboard name" disabled={!active} value={active ? dashboardName : 'New dashboard'} onChange={event => setDashboardName(event.target.value)} className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none placeholder:text-muted-foreground disabled:cursor-default"/><button type="button" aria-label="Edit dashboard name" title="Edit dashboard name" disabled={!active} onClick={() => titleInputRef.current?.focus()} className="cursor-pointer rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-40"><Pencil size={15}/></button></div><p className="mt-1 text-sm text-muted-foreground">{nameSave.pending ? 'Saving dashboard name…' : active?.description || 'DuckDB SQL telemetry views, kept locally with your traces.'}</p></div><button onClick={() => void deleteActive()} disabled={!active} className="cursor-pointer rounded border border-danger px-3 py-2 text-xs text-danger disabled:opacity-40">Delete dashboard</button></header>{!active ? <div className="p-8"><label className="block max-w-md text-sm font-medium">Dashboard name<input value={name} onChange={e=>setName(e.target.value)} placeholder={defaultName} className="mt-2 w-full rounded border border-input bg-background px-3 py-2"/></label><button onClick={create} className="mt-4 cursor-pointer rounded bg-accent px-3 py-2 text-sm text-accent-ink">Create dashboard</button></div> : <><section className="mx-6 mt-6 rounded-lg border border-border bg-surface"><div className="border-b border-border px-4 py-3"><h2 className="font-semibold">{editing ? 'Edit panel' : 'SQL panel query'}</h2><p className="mt-1 text-xs text-muted-foreground">Read-only DuckDB SQL against telemetry_spans, telemetry_logs, telemetry_metrics, and telemetry_traces.</p></div><div className="grid gap-3 p-4 sm:grid-cols-2"><label className="block text-xs font-medium">Panel title<input value={title} onChange={e=>setTitle(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm"/></label><label className="block text-xs font-medium">Display<select value={display} onChange={e=>setDisplay(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm">{displays.map(x=><option key={x}>{x}</option>)}</select></label><label className="block text-xs font-medium sm:col-span-2">DuckDB SQL<SqlEditor value={query} onChange={setQuery}/></label><div className="sm:col-span-2 flex flex-wrap justify-end gap-2"><button onClick={() => preview.mutate()} disabled={preview.isPending} className="cursor-pointer rounded border border-border px-3 py-2 text-sm">{preview.isPending ? 'Running preview…' : 'Run preview'}</button><button onClick={savePanel} className="cursor-pointer rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink">{editing ? 'Save changes' : 'Add panel'}</button></div>{preview.isError&&<p role="alert" className="sm:col-span-2 text-sm text-danger">Preview failed: {preview.error.message}</p>}{preview.data&&<div className="sm:col-span-2 rounded border border-border bg-background p-3"><p className="font-mono text-[10px] text-muted-foreground">PREVIEW · {preview.data.rows.length} rows · {preview.data.columns.join(', ')}</p><pre className="mt-2 max-h-36 overflow-auto text-[11px]">{JSON.stringify(preview.data.rows.slice(0, 5), null, 2)}</pre></div>}</div></section><div className="grid gap-4 p-6 xl:grid-cols-2">{active.panels.map(p => <Panel key={p.id} panel={p} dashboardId={active.id} refresh={refresh} onEdit={edit}/>)}{active.panels.length === 0 && <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">Use the SQL editor to add the first panel.</div>}</div></>}</main>
    <aside className="w-[360px] shrink-0 overflow-auto border-l border-border bg-surface p-4"><section><h2 className="text-sm font-semibold">Telemetry SQL</h2><p className="mt-1 text-xs text-muted-foreground">Queries run read-only against the stable telemetry views. Use <code>$name</code> for a dashboard parameter.</p><input aria-label="Search telemetry SQL examples" value={catalogSearch} onChange={e => setCatalogSearch(e.target.value)} placeholder="Search query examples" className="mt-3 w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/>{catalog.data?.map(item => <button key={`${item.signal}-${item.name}`} onClick={() => { setTitle(item.name); setQuery(item.query); setDisplay(item.display_type) }} className="mt-2 w-full rounded border border-border bg-background p-2 text-left hover:bg-muted"><span className="block text-xs font-medium">{item.name}</span><code className="mt-1 block truncate text-[10px] text-muted-foreground">{item.query}</code></button>)}</section><section className="mt-6 border-t border-border pt-4"><h2 className="flex items-center gap-2 text-sm font-semibold"><Variable size={14}/> Named parameters</h2><p className="mt-1 text-xs text-muted-foreground">Add bounded values, then reference them as <code>$name</code> in DuckDB SQL.</p><div className="mt-3 grid gap-2"><input aria-label="Variable name" value={variableName} onChange={e=>setVariableName(e.target.value)} placeholder="customer_tier" className="w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/><div className="grid grid-cols-2 gap-2"><select aria-label="Variable datatype" value={variableKind} onChange={e=>setVariableKind(e.target.value as DashboardVariable['kind'])} className="rounded border border-input bg-background px-2 py-1.5 text-xs"><option value="string">string</option><option value="number">number</option><option value="boolean">boolean</option><option value="duration">duration</option><option value="time">time range</option><option value="enum">enum</option></select><input aria-label="Variable default value" value={variableDefault} onChange={e=>setVariableDefault(e.target.value)} placeholder="Default value" className="min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs"/></div><input aria-label="Variable value source" value={variableSource} onChange={e=>setVariableSource(e.target.value)} placeholder={variableKind === 'enum' ? 'free, team, enterprise' : 'attributes.user.plan'} className="w-full rounded border border-input bg-background px-2 py-1.5 font-mono text-xs"/><button onClick={addVariable} disabled={!active || !variableName.trim() || !variableSource.trim()} className="cursor-pointer rounded border border-border px-2 py-1.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">Add parameter</button></div>{active?.variables.map(v=><button key={v.name} onClick={() => setQuery(current => `${current}$${v.name}`)} className="mt-2 grid w-full grid-cols-[90px_1fr_60px] gap-2 truncate border-b border-border px-2 py-2 text-left font-mono text-[10px] hover:bg-muted" title={`Insert $${v.name}`}><span>${v.name}</span><span className="truncate">{v.source}</span><span>{v.kind}</span></button>)}</section>
      <section className="mt-6 border-t border-border pt-4"><h2 className="text-sm font-semibold">Draft canvas</h2><p className="mt-1 text-xs text-muted-foreground">{active ? `${active.panels.length} SQL panel${active.panels.length === 1 ? '' : 's'}` : 'Create a dashboard to begin.'}</p><div className="mt-3 space-y-2">{active?.panels.map(panel => <button key={panel.id} onClick={() => edit(panel)} className="w-full cursor-pointer rounded border border-border bg-background p-3 text-left hover:bg-muted"><span className="block truncate text-xs font-medium">{panel.title}</span><span className="mt-1 block truncate font-mono text-[10px] text-muted-foreground">{panel.query_sql}</span></button>)}{active?.panels.length === 0 && <div className="rounded border border-dashed border-border p-4 text-xs text-muted-foreground">Panels you add appear here.</div>}</div></section></aside>
  </div>
}
