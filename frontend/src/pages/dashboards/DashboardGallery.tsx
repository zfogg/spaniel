import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api, type Dashboard } from '@/lib/api'
import { qk } from '@/lib/query'
import { toast } from 'sonner'
import { DashboardList } from '@/components/dashboard-panels/DashboardList'
import { DashboardPanel as Panel } from './DashboardPanel'
import { readLocalDashboards } from './dashboard-model'
export function DashboardGallery() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { data: savedDashboards = [] } = useQuery({
    queryKey: qk.dashboards(),
    queryFn: () => api.dashboards.list().then((x) => x.data),
  })
  const [local] = useState<Dashboard[]>(readLocalDashboards)
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedId = searchParams.get('id')
  const [reordering, setReordering] = useState(false)
  const reorderPending = useRef(false)
  const dashboards = useMemo(
    () => [
      ...savedDashboards,
      ...local.filter(
        (localDashboard) => !savedDashboards.some((saved) => saved.id === localDashboard.id),
      ),
    ],
    [savedDashboards, local],
  )
  const selected = dashboards.find((dashboard) => dashboard.id === selectedId) ?? dashboards[0]
  const [variables, setVariables] = useState<Record<string, string>>({})
  const [panelStatus, setPanelStatus] = useState<
    Record<string, { state: 'loading' | 'ready' | 'error'; message?: string }>
  >({})
  useEffect(() => {
    if (selected)
      setVariables(
        Object.fromEntries(
          selected.variables.map((variable) => [variable.name, variable.default_value]),
        ),
      )
  }, [selected])
  const dashboardHref = (id: string, index: number) => {
    const next = new URLSearchParams(searchParams)
    if (index === 0) next.delete('id')
    else next.set('id', id)
    return `/dashboards${next.size ? `?${next}` : ''}`
  }
  const reorder = async (from: string, to: string) => {
    if (from === to || reorderPending.current) return
    const next = [...dashboards]
    const source = next.findIndex((item) => item.id === from),
      target = next.findIndex((item) => item.id === to)
    if (source < 0 || target < 0) return
    next.splice(target, 0, ...next.splice(source, 1))
    reorderPending.current = true
    setReordering(true)
    try {
      await api.dashboards.reorder(
        next.filter((item) => !item.id.startsWith('local-')).map((item) => item.id),
      )
      // Preserve the viewed dashboard when moving a different entry to first.
      setSearchParams(
        (current) => {
          const params = new URLSearchParams(current)
          if (selected && selected.id !== next[0]?.id) params.set('id', selected.id)
          else params.delete('id')
          return params
        },
        { replace: true },
      )
      qc.setQueryData(
        qk.dashboards(),
        next.filter((item) => !item.id.startsWith('local-')),
      )
      await qc.invalidateQueries({ queryKey: qk.dashboards() })
    } catch (error) {
      toast.error(
        `Could not reorder dashboards: ${error instanceof Error ? error.message : String(error)}`,
      )
    } finally {
      reorderPending.current = false
      setReordering(false)
    }
  }
  return (
    <div className="dashboard-workspace flex min-h-0 flex-1 overflow-hidden bg-background text-foreground">
      <aside className="w-56 shrink-0 overflow-auto border-r border-border bg-surface">
        <p className="px-4 pb-2 pt-5 font-mono text-[10px] uppercase tracking-[.12em] text-muted-foreground">
          Dashboards
        </p>
        <DashboardList
          dashboards={dashboards}
          selectedId={selected?.id}
          href={dashboardHref}
          reorder={reorder}
          pending={reordering}
        />
        <Link
          to="/dashboards/new"
          className="mx-3 mt-3 flex cursor-pointer items-center justify-center gap-1 rounded-md border border-dashed border-accent px-2 py-2 text-xs font-semibold text-accent-ink"
        >
          <Plus size={14} /> New dashboard
        </Link>
      </aside>
      <main className="min-w-0 flex-1 overflow-auto">
        <header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5">
          <div>
            <h1 className="text-[22px] font-semibold tracking-tight">
              {selected?.name ?? 'Dashboards'}
            </h1>
            <p className="mt-1 text-xs text-muted-foreground">
              {selected?.description || 'Query-backed telemetry views.'}
            </p>
          </div>
          <div className="ml-auto">
            <button
              disabled={selected?.id.startsWith('file-')}
              onClick={() => selected && navigate(`/dashboards/${selected.id}`)}
              className="cursor-pointer rounded-md bg-[#315b7d] px-3 py-2 text-xs font-medium text-white hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {selected?.id.startsWith('file-') ? 'File-managed' : 'Edit dashboard'}
            </button>
          </div>
        </header>
        {selected ? (
          <div className="mx-auto max-w-6xl p-5">
            {Object.values(panelStatus).some((status) => status.state === 'error') && (
              <p
                role="alert"
                className="mb-4 rounded border border-danger/40 bg-danger/10 px-3 py-2 text-xs text-danger"
              >
                Partial dashboard data:{' '}
                {Object.values(panelStatus).filter((status) => status.state === 'error').length}{' '}
                panel query
                {Object.values(panelStatus).filter((status) => status.state === 'error').length ===
                1
                  ? ''
                  : 'ies'}{' '}
                failed. Healthy panels are still shown.
              </p>
            )}
            {selected.id.startsWith('file-') && (
              <p className="mb-4 rounded border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                File-managed dashboard: its YAML file is authoritative and reloads on startup.
                Export/import its YAML to create an editable local copy.
              </p>
            )}
            <div className="mb-4 flex flex-wrap gap-2">
              {selected.variables.map((variable) => (
                <label key={variable.name} className="font-mono text-[11px] text-muted-foreground">
                  ${variable.name}
                  <input
                    aria-label={`Dashboard variable ${variable.name}`}
                    value={variables[variable.name] ?? ''}
                    onChange={(event) =>
                      setVariables((current) => ({
                        ...current,
                        [variable.name]: event.target.value,
                      }))
                    }
                    className="ml-1 rounded border border-border bg-surface px-2 py-1"
                  />
                </label>
              ))}
            </div>
            <div className="grid auto-rows-[420px] gap-4 lg:grid-cols-12">
              {selected.panels.map((panel) => (
                <Panel
                  key={panel.id}
                  panel={panel}
                  dashboardId={selected.id}
                  variables={variables}
                  onStatus={(id, state, message) =>
                    setPanelStatus((current) =>
                      current[id]?.state === state && current[id]?.message === message
                        ? current
                        : { ...current, [id]: { state, message } },
                    )
                  }
                />
              ))}
              {selected.panels.length === 0 && (
                <div className="rounded-lg border border-dashed border-border p-12 text-center text-sm text-muted-foreground">
                  This dashboard has no panels yet. Select <b>Edit dashboard</b> to add one.
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="p-12 text-center">
            <h2 className="text-lg font-semibold">No dashboards yet</h2>
            <Link
              to="/dashboards/new"
              className="mt-4 inline-flex rounded bg-accent px-3 py-2 text-sm text-accent-ink"
            >
              Create dashboard
            </Link>
          </div>
        )}
      </main>
    </div>
  )
}
