import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { LayoutDashboard, Plus, Trash2, Variable, Pencil } from 'lucide-react'
import { api, type Dashboard, type DashboardPanel, type DashboardVariable } from '@/lib/api'
import { qk } from '@/lib/query'
import { useDebouncedSave } from '@/lib/useDebouncedSave'
import { toast } from 'sonner'

const displays = ['single_value', 'time_series', 'table', 'heatmap', 'trace_list', 'log_list'] as const
const magic = [
  { name: '$service_name', sample: 'checkout-api · inventory', type: 'service' },
  { name: '$environment', sample: 'development · production', type: 'enum' },
  { name: '$window', sample: '30m · 1h · 24h', type: 'time' },
  { name: '$operation_name', sample: 'POST /orders · SELECT inventory', type: 'operation' },
  { name: '$status', sample: 'error · ok · unset', type: 'enum' },
  { name: '$region', sample: 'us-east-1 · us-west-2', type: 'string' },
  { name: '$deployment_version', sample: 'v1.42.0', type: 'string' },
  { name: '$selected_trace_id', sample: 'selected trace', type: 'trace_id' },
  { name: '$selected_span_id', sample: 'selected span', type: 'span_id' },
  { name: '$selected_log_id', sample: 'selected log', type: 'log_id' },
]
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

function Panel({ panel, dashboardId, refresh, onEdit }: { panel: DashboardPanel; dashboardId: string; refresh: () => void; onEdit: (p: DashboardPanel) => void }) {
  const [preview, setPreview] = useState<Record<string, unknown>[]>([])
  const run = async () => setPreview((await api.dashboards.preview(dashboardId, { query_text: panel.query_text, display_type: panel.display_type })).data.rows)
  const remove = async () => { await api.dashboards.removePanel(dashboardId, panel.id); refresh() }
  return <article className="min-h-[180px] rounded-lg border border-border bg-surface shadow-sm overflow-hidden">
    <header className="flex items-start gap-2 border-b border-border px-4 py-3"><div className="min-w-0 flex-1"><h2 className="truncate font-semibold text-[13px]">{panel.title}</h2><p className="mt-1 font-mono text-[10px] text-muted-foreground truncate">{panel.display_type} · {panel.query_text}</p></div><button aria-label={`Delete ${panel.title}`} onClick={remove} className="cursor-pointer text-muted-foreground hover:text-danger"><Trash2 size={14}/></button></header>
    <div className="p-4"><button onClick={run} className="cursor-pointer rounded border border-border bg-muted px-2 py-1 font-mono text-[10px] hover:bg-surface2">Run panel</button><button onClick={() => onEdit(panel)} className="ml-2 cursor-pointer rounded border border-border px-2 py-1 font-mono text-[10px] hover:bg-muted">Edit</button>{preview.length > 0 && <PanelResult display={panel.display_type} rows={preview}/>}</div>
  </article>
}

function panelValue(row: Record<string, unknown>, key = 'value') { const value = row[key]; return typeof value === 'number' ? value.toLocaleString(undefined, { maximumFractionDigits: 2 }) : String(value ?? '—') }
function PanelResult({ display, rows }: { display: DashboardPanel['display_type']; rows: Record<string, unknown>[] }) {
  if (display === 'single_value') return <div className="mt-4 rounded bg-surface2 p-4"><p className="font-mono text-2xl font-semibold">{panelValue(rows[0])}</p><p className="mt-1 text-[10px] text-muted-foreground">Current value</p></div>
  if (display === 'trace_list' || display === 'log_list') return <ul className="mt-4 max-h-32 divide-y divide-border overflow-auto rounded border border-border">{rows.slice(0, 8).map((row, index) => <li key={index} className="flex gap-2 px-2 py-1.5 font-mono text-[10px]"><span className="shrink-0 text-muted-foreground">{panelValue(row, display === 'log_list' ? 'severity' : 'service_name')}</span><span className="truncate">{panelValue(row, display === 'log_list' ? 'body' : 'name')}</span></li>)}</ul>
  if (display === 'heatmap') return <div className="mt-4 flex h-24 items-end gap-1 rounded bg-surface2 p-2">{rows.slice(0, 32).map((row, index) => <div key={index} title={panelValue(row)} className="min-w-1 flex-1 rounded-t bg-accent" style={{ height: `${Math.max(8, Math.min(100, Number(row.value ?? 0)))}%`, opacity: 0.35 + (index % 5) * 0.12 }}/>)}</div>
  if (display === 'time_series') return <div className="mt-4 flex h-24 items-end gap-1 rounded bg-surface2 p-2">{rows.slice(0, 24).map((row, index) => <div key={index} title={panelValue(row)} className="min-w-1 flex-1 rounded-t bg-accent" style={{ height: `${Math.max(6, Math.min(100, Number(row.value ?? 0)))}%` }}/>)}</div>
  const columns = Object.keys(rows[0] ?? {})
  return <div className="mt-4 max-h-32 overflow-auto rounded border border-border"><table className="w-full text-left font-mono text-[10px]"><thead className="sticky top-0 bg-surface2"><tr>{columns.map(column => <th key={column} className="px-2 py-1 font-medium">{column}</th>)}</tr></thead><tbody>{rows.slice(0, 10).map((row, index) => <tr key={index} className="border-t border-border">{columns.map(column => <td key={column} className="max-w-40 truncate px-2 py-1">{panelValue(row, column)}</td>)}</tr>)}</tbody></table></div>
}

export function DashboardEditor() {
  const { dashboardId } = useParams()
  const editorNavigate = useNavigate()
  const qc = useQueryClient(); const refresh = () => qc.invalidateQueries({ queryKey: qk.dashboards() })
  const { data: savedDashboards = [] } = useQuery({ queryKey: qk.dashboards(), queryFn: () => api.dashboards.list().then(x => x.data) })
  const [localDashboards, setLocalDashboards] = useState<Dashboard[]>(readLocalDashboards)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const [selected, setSelected] = useState<string | null>(dashboardId ?? null); const [name, setName] = useState(''); const [description, setDescription] = useState(''); const [dashboardName, setDashboardName] = useState(''); const [query, setQuery] = useState('p95(spans.duration) by service_name'); const [title, setTitle] = useState('p95 duration'); const [display, setDisplay] = useState<string>('time_series'); const [editing, setEditing] = useState<DashboardPanel | null>(null); const [variableName, setVariableName] = useState('customer_tier'); const [variableSource, setVariableSource] = useState('attributes.user.plan'); const [variableKind, setVariableKind] = useState<DashboardVariable['kind']>('string'); const [variableDefault, setVariableDefault] = useState('')
  const dashboards = [...savedDashboards, ...localDashboards.filter(localDashboard => !savedDashboards.some(saved => saved.id === localDashboard.id))]
  const active = useMemo(() => dashboards.find(x => x.id === (dashboardId ?? selected)) ?? (dashboardId ? undefined : dashboards[0]), [dashboards, dashboardId, selected])
  const defaultName = nextDashboardName(dashboards)
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
    const body = { title, display_type: display as DashboardPanel['display_type'], query_text: query, position: editing?.position ?? active.panels.length }
    const saveLocalPanel = () => {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = editing
        ? { ...editing, ...body, updated_at: now }
        : { id: `local-panel-${crypto.randomUUID()}`, dashboard_id: active.id, ...body, query_json: '', settings_json: '{}', layout_json: '{}', updated_at: now }
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
      throw error
    }
    setEditing(null)
  }
  const edit = (p: DashboardPanel) => { setEditing(p); setTitle(p.title); setQuery(p.query_text); setDisplay(p.display_type) }
  const addVariable = async () => {
    if (!active || !variableName.trim() || !variableSource.trim()) return
    const body = { name: variableName.trim(), kind: variableKind, source: variableSource.trim(), default_value: variableDefault.trim(), options_json: variableKind === 'enum' ? JSON.stringify(variableSource.split(',').map(value => value.trim()).filter(Boolean)) : '[]' }
    try {
      await api.dashboards.variable(active.id, body)
      refresh()
    } catch {
      if (!active.id.startsWith('local-')) return
      setLocalDashboards(current => {
        const next = current.map(dashboard => dashboard.id !== active.id ? dashboard : { ...dashboard, variables: [...dashboard.variables.filter(item => item.name !== body.name), { dashboard_id: active.id, ...body }] })
        saveLocalDashboards(next)
        return next
      })
    }
  }
  const insertMagic = (v: string) => setQuery(q => q + (q ? ' ' : '') + v)
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
    <main className="flex-1 overflow-auto bg-background"><header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5"><div className="min-w-0 flex-1"><div className="flex max-w-xl items-center gap-1"><input ref={titleInputRef} aria-label="Dashboard name" disabled={!active} value={active ? dashboardName : 'New dashboard'} onChange={event => setDashboardName(event.target.value)} className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none placeholder:text-muted-foreground disabled:cursor-default"/><button type="button" aria-label="Edit dashboard name" title="Edit dashboard name" disabled={!active} onClick={() => titleInputRef.current?.focus()} className="cursor-pointer rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-40"><Pencil size={15}/></button></div><p className="mt-1 text-sm text-muted-foreground">{nameSave.pending ? 'Saving dashboard name…' : active?.description || 'Query-backed telemetry views, kept locally with your traces.'}</p></div><button onClick={() => void deleteActive()} disabled={!active} className="cursor-pointer rounded border border-danger px-3 py-2 text-xs text-danger disabled:opacity-40">Delete dashboard</button></header>{!active ? <div className="p-8"><label className="block max-w-md text-sm font-medium">Dashboard name<input value={name} onChange={e=>setName(e.target.value)} placeholder={defaultName} className="mt-2 w-full rounded border border-input bg-background px-3 py-2"/></label><button onClick={create} className="mt-4 cursor-pointer rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink">Create dashboard</button></div> : <><section className="mx-6 mt-6 rounded-lg border border-border bg-surface"><div className="border-b border-border px-4 py-3"><h2 className="font-semibold">{editing ? 'Edit panel' : 'Query composer'}</h2><p className="mt-1 text-xs text-muted-foreground">Only Spaniel’s typed query language runs here.</p></div><div className="grid gap-3 p-4 sm:grid-cols-2"><label className="block text-xs font-medium">Panel title<input value={title} onChange={e=>setTitle(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm"/></label><label className="block text-xs font-medium">Display<select value={display} onChange={e=>setDisplay(e.target.value)} className="mt-1 w-full rounded border border-input bg-background px-2 py-1.5 text-sm">{displays.map(x=><option key={x}>{x}</option>)}</select></label><label className="block text-xs font-medium sm:col-span-2">Query<textarea value={query} onChange={e=>setQuery(e.target.value)} className="mt-1 h-24 w-full rounded border border-input bg-background p-2 font-mono text-xs"/></label><div className="sm:col-span-2 flex justify-end"><button onClick={savePanel} className="cursor-pointer rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink">{editing ? 'Save changes' : 'Add panel'}</button></div></div></section><div className="grid gap-4 p-6 xl:grid-cols-2">{active.panels.map(p => <Panel key={p.id} panel={p} dashboardId={active.id} refresh={refresh} onEdit={edit}/>)}{active.panels.length === 0 && <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">Use the composer to add the first panel.</div>}</div></>}</main>
    <aside className="w-[360px] shrink-0 overflow-auto border-l border-border bg-surface p-4"><section><h2 className="flex items-center gap-2 text-sm font-semibold"><Variable size={14}/> Reusable variables</h2><p className="mt-1 text-xs text-muted-foreground">Define a name, a bounded value source, and a datatype.</p><div className="mt-3 grid gap-2"><input aria-label="Variable name" value={variableName} onChange={e=>setVariableName(e.target.value)} placeholder="customer_tier" className="w-full rounded border border-input bg-background px-2 py-1.5 text-xs"/><div className="grid grid-cols-2 gap-2"><select aria-label="Variable datatype" value={variableKind} onChange={e=>setVariableKind(e.target.value as DashboardVariable['kind'])} className="rounded border border-input bg-background px-2 py-1.5 text-xs"><option value="string">string</option><option value="number">number</option><option value="boolean">boolean</option><option value="duration">duration</option><option value="time">time range</option><option value="enum">enum</option></select><input aria-label="Variable default value" value={variableDefault} onChange={e=>setVariableDefault(e.target.value)} placeholder="Default value" className="min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs"/></div><input aria-label="Variable value source" value={variableSource} onChange={e=>setVariableSource(e.target.value)} placeholder={variableKind === 'enum' ? 'free, team, enterprise' : 'attributes.user.plan'} className="w-full rounded border border-input bg-background px-2 py-1.5 font-mono text-xs"/><button onClick={addVariable} disabled={!active || !variableName.trim() || !variableSource.trim()} className="cursor-pointer rounded border border-border px-2 py-1.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50">Add reusable variable</button></div>{active?.variables.map(v=><div key={v.name} className="mt-2 grid grid-cols-[90px_1fr_60px] gap-2 truncate border-b border-border px-2 py-2 font-mono text-[10px]" title={`${v.name}: ${v.source}`}><span>${v.name}</span><span className="truncate">{v.source}</span><span>{v.kind}</span></div>)}</section>
      <section className="mt-6 border-t border-border pt-4"><h2 className="text-sm font-semibold">Magic variables</h2><p className="mt-1 text-xs text-muted-foreground">Click a value to insert fixed context into the query.</p>{magic.map(v=><button key={v.name} onClick={()=>insertMagic(v.name)} className="mt-2 grid w-full cursor-pointer grid-cols-[110px_1fr_62px] gap-2 border-b border-border px-2 py-2 text-left font-mono text-[10px] hover:bg-muted"><span className="truncate">{v.name}</span><span className="truncate text-muted-foreground" title={v.sample}>{v.sample}</span><span className="text-right text-accent">{v.type}</span></button>)}</section>
      <section className="mt-6 border-t border-border pt-4"><h2 className="text-sm font-semibold">Draft canvas</h2><p className="mt-1 text-xs text-muted-foreground">{active ? `${active.panels.length} query-backed panel${active.panels.length === 1 ? '' : 's'}` : 'Create a dashboard to begin.'}</p><div className="mt-3 space-y-2">{active?.panels.map(panel => <button key={panel.id} onClick={() => edit(panel)} className="w-full cursor-pointer rounded border border-border bg-background p-3 text-left hover:bg-muted"><span className="block truncate text-xs font-medium">{panel.title}</span><span className="mt-1 block truncate font-mono text-[10px] text-muted-foreground">{panel.query_text}</span></button>)}{active?.panels.length === 0 && <div className="rounded border border-dashed border-border p-4 text-xs text-muted-foreground">Panels you add appear here.</div>}</div></section></aside>
  </div>
}
