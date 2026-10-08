import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  api,
  type Dashboard,
  type DashboardPanel,
  type DashboardVariable,
  type QueryCatalogEntry,
} from '@/lib/api'
import { qk } from '@/lib/query'
import { useDebouncedSave } from '@/lib/useDebouncedSave'
import { toast } from 'sonner'
import { type PanelLayout } from '@/components/dashboard-panels/DashboardCanvas'
import { dashboardTemplates } from '@/components/dashboard-panels/dashboard-templates'
import {
  readLocalDashboards,
  saveLocalDashboards,
  backendUnavailable,
  nextDashboardName,
} from './dashboard-model'
export function useDashboardEditor() {
  const { dashboardId } = useParams()
  const editorNavigate = useNavigate()
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: qk.dashboards() })
  const { data: savedDashboards = [] } = useQuery({
    queryKey: qk.dashboards(),
    queryFn: () => api.dashboards.list().then((x) => x.data),
  })
  const [localDashboards, setLocalDashboards] = useState<Dashboard[]>(readLocalDashboards)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedTab = searchParams.get('tab')
  const editorTab =
    requestedTab === 'library' || requestedTab === 'panels' ? requestedTab : 'design'
  const setEditorTab = (tab: string) => {
    if (tab !== 'design' && tab !== 'library' && tab !== 'panels') return
    setSearchParams((current) => {
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
  useEffect(() => {
    if (!dashboardId) {
      setTemplateId('none')
      setCreateError('')
      setImportError('')
    }
  }, [dashboardId])
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [dashboardName, setDashboardName] = useState('')
  const [query, setQuery] = useState(
    'SELECT (start_ns // 60000000000) * 60000000000 AS timestamp_ns, count(*) AS value FROM spans GROUP BY 1 ORDER BY 1',
  )
  const [title, setTitle] = useState('Span count')
  const [display, setDisplay] = useState<string>('time_series')
  const [settingsJSON, setSettingsJSON] = useState('{}')
  const [layout, setLayout] = useState({ x: 1, y: 1, w: 6, h: 1 })
  const [editing, setEditing] = useState<DashboardPanel | null>(null)
  const [variableName, setVariableName] = useState('service')
  const [variableSource, setVariableSource] = useState('telemetry_spans.service_name')
  const [variableKind, setVariableKind] = useState<DashboardVariable['kind']>('string')
  const [variableDefault, setVariableDefault] = useState('')
  const [catalogSearch, setCatalogSearch] = useState('')
  const [configText, setConfigText] = useState<string | null>(null)
  const [configError, setConfigError] = useState<string | null>(null)
  const dashboards = useMemo(
    () => [
      ...savedDashboards,
      ...localDashboards.filter(
        (localDashboard) => !savedDashboards.some((saved) => saved.id === localDashboard.id),
      ),
    ],
    [savedDashboards, localDashboards],
  )
  // /dashboards/new is a real creation canvas, not an implicit edit of the
  // first saved dashboard.
  // The creation route must never inherit a dashboard selected earlier in this
  // mounted editor instance. Its URL is the source of truth.
  const active = useMemo(
    () => (dashboardId ? dashboards.find((x) => x.id === dashboardId) : undefined),
    [dashboards, dashboardId],
  )
  const defaultName = nextDashboardName(dashboards)
  const [debouncedSearch, setDebouncedSearch] = useState(catalogSearch)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(catalogSearch.trim()), 250)
    return () => window.clearTimeout(timer)
  }, [catalogSearch])
  const activeSession = useQuery({
    queryKey: qk.activeSession(),
    queryFn: () => api.sessions.getActive().then((response) => response.data),
    enabled: !!active,
    refetchInterval: 5000,
    staleTime: 5000,
  })
  const catalog = useQuery({
    queryKey: ['query-catalog', debouncedSearch, activeSession.data?.id],
    queryFn: ({ signal }) =>
      api.dashboards.catalog(undefined, debouncedSearch, signal).then((x) => x.data),
    retry: 1,
    staleTime: 15_000,
  })
  const preview = useMutation({
    mutationFn: () =>
      active
        ? api.dashboards
            .preview(active.id, {
              query_sql: query,
              name: title,
              display_type: display,
            })
            .then((x) => ({ ...x.data, querySQL: query, displayType: display }))
        : Promise.reject(new Error('Create a dashboard before previewing SQL')),
  })
  useEffect(() => {
    setDashboardName(active?.name ?? '')
  }, [active?.name])
  const saveDashboardName = async (nextName: string) => {
    if (!active || !nextName.trim() || nextName.trim() === active.name) return
    const name = nextName.trim()
    if (active.id.startsWith('local-')) {
      setLocalDashboards((current) => {
        const next = current.map((dashboard) =>
          dashboard.id === active.id
            ? { ...dashboard, name, updated_at: Date.now() * 1_000_000 }
            : dashboard,
        )
        saveLocalDashboards(next)
        return next
      })
      return
    }
    await api.dashboards.update(active.id, {
      name,
      description: active.description,
    })
    refresh()
  }
  const nameSave = useDebouncedSave({
    key: active?.id ?? 'new-dashboard',
    value: dashboardName,
    enabled: !!active,
    save: saveDashboardName,
  })
  const create = async () => {
    if (creatingRef.current) return
    creatingRef.current = true
    setCreating(true)
    setCreateError('')
    const template =
      dashboardTemplates.find((item) => item.id === templateId) ?? dashboardTemplates[0]
    const body = {
      name: name.trim() || (template.id === 'none' ? defaultName : template.name),
      description: description.trim() || template.description,
      panels: template.panels,
    }
    try {
      const result = await api.dashboards.create(body)
      qc.setQueryData<Dashboard[]>(qk.dashboards(), (current) => [...(current ?? []), result.data])
      void refresh()
      editorNavigate(`/dashboards/${result.data.id}`)
    } catch (error) {
      setCreateError(
        `Could not create dashboard: ${error instanceof Error ? error.message : String(error)}. Your selections are preserved.`,
      )
    } finally {
      creatingRef.current = false
      setCreating(false)
    }
  }
  const importYAML = async (yaml: string) => {
    if (creatingRef.current || importing) return
    setImporting(true)
    setImportError('')
    try {
      const result = await api.dashboards.importConfig(yaml)
      qc.setQueryData<Dashboard[]>(qk.dashboards(), (current) => [...(current ?? []), result.data])
      void refresh()
      editorNavigate(`/dashboards/${result.data.id}`)
    } catch (error) {
      setImportError(
        `Could not import dashboard: ${error instanceof Error ? error.message : String(error)}`,
      )
    } finally {
      setImporting(false)
    }
  }
  const savePanel = async () => {
    if (!active) return
    try {
      JSON.parse(settingsJSON)
    } catch {
      toast.error('Panel settings must be valid JSON.')
      return
    }
    const body = {
      title,
      display_type: display as DashboardPanel['display_type'],
      query_sql: query,
      position: editing?.position ?? active.panels.length,
      settings_json: settingsJSON,
      layout_json: JSON.stringify(layout),
    }
    const saveLocalPanel = () => {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = editing
        ? { ...editing, ...body, updated_at: now }
        : {
            id: `local-panel-${crypto.randomUUID()}`,
            dashboard_id: active.id,
            ...body,
            query_version: 1,
            updated_at: now,
          }
      setLocalDashboards((current) => {
        const next = current.map((dashboard) =>
          dashboard.id !== active.id
            ? dashboard
            : {
                ...dashboard,
                updated_at: now,
                panels: editing
                  ? dashboard.panels.map((item) => (item.id === panel.id ? panel : item))
                  : [...dashboard.panels, panel],
              },
        )
        saveLocalDashboards(next)
        return next
      })
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
    const body = {
      title: item.name,
      display_type: item.display_type as DashboardPanel['display_type'],
      query_sql: item.query,
      position: active.panels.length,
    }
    setTitle(body.title)
    setQuery(body.query_sql)
    setDisplay(body.display_type)
    if (active.id.startsWith('local-')) {
      const now = Date.now() * 1_000_000
      const panel: DashboardPanel = {
        id: `local-panel-${crypto.randomUUID()}`,
        dashboard_id: active.id,
        ...body,
        query_version: 1,
        settings_json: '{}',
        layout_json: '{}',
        updated_at: now,
      }
      setLocalDashboards((current) => {
        const next = current.map((dashboard) =>
          dashboard.id === active.id
            ? {
                ...dashboard,
                updated_at: now,
                panels: [...dashboard.panels, panel],
              }
            : dashboard,
        )
        saveLocalDashboards(next)
        return next
      })
      return
    }
    try {
      await api.dashboards.panel(active.id, body)
      await refresh()
      toast.success(`Added “${item.name}” to this dashboard`)
    } catch (error) {
      toast.error(
        `Could not create panel: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }
  const edit = (p: DashboardPanel) => {
    setEditorTab('design')
    setEditing(p)
    setTitle(p.title)
    setQuery(p.query_sql)
    setDisplay(p.display_type)
    setSettingsJSON(p.settings_json || '{}')
    try {
      const saved = JSON.parse(p.layout_json) as {
        x?: number
        y?: number
        w?: number
        h?: number
        width?: string
      }
      setLayout({
        x: saved.x ?? 1,
        y: saved.y ?? 1,
        w: saved.w ?? (saved.width === 'wide' ? 12 : 6),
        h: saved.h ?? 1,
      })
    } catch {
      setLayout({ x: 1, y: 1, w: 6, h: 1 })
    }
  }
  const [movingPanel, setMovingPanel] = useState(false)
  const movePending = useRef(false)
  const movePanel = async (panel: DashboardPanel, direction: -1 | 1) => {
    if (!active || movePending.current) return
    movePending.current = true
    setMovingPanel(true)
    try {
      if (active.id.startsWith('local-')) {
        setLocalDashboards((current) => {
          const next = current.map((dashboard) => {
            if (dashboard.id !== active.id) return dashboard
            const panels = [...dashboard.panels]
            const index = panels.findIndex((item) => item.id === panel.id)
            const target = index + direction
            if (index < 0 || target < 0 || target >= panels.length) return dashboard
            ;[panels[index], panels[target]] = [panels[target], panels[index]]
            return {
              ...dashboard,
              panels: panels.map((item, position) => ({ ...item, position })),
            }
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
    const body = {
      title: panel.title,
      display_type: panel.display_type,
      query_sql: panel.query_sql,
      position: panel.position,
      settings_json: panel.settings_json,
      layout_json: JSON.stringify(nextLayout),
    }
    setMovingPanel(true)
    try {
      if (active.id.startsWith('local-')) {
        setLocalDashboards((current) => {
          const next = current.map((dashboard) =>
            dashboard.id !== active.id
              ? dashboard
              : {
                  ...dashboard,
                  panels: dashboard.panels.map((item) =>
                    item.id === panel.id
                      ? {
                          ...item,
                          layout_json: body.layout_json,
                          updated_at: Date.now() * 1_000_000,
                        }
                      : item,
                  ),
                },
          )
          saveLocalDashboards(next)
          return next
        })
      } else {
        await api.dashboards.updatePanel(active.id, panel.id, body)
        await refresh()
      }
    } catch (error) {
      toast.error(
        `Could not save panel layout: ${error instanceof Error ? error.message : String(error)}`,
      )
    } finally {
      setMovingPanel(false)
    }
  }
  const addVariable = async () => {
    if (!active || !variableName.trim() || !variableSource.trim()) return
    const body = {
      name: variableName.trim(),
      kind: variableKind,
      source: variableSource.trim(),
      default_value: variableDefault.trim(),
      options_json:
        variableKind === 'enum'
          ? JSON.stringify(
              variableSource
                .split(',')
                .map((value) => value.trim())
                .filter(Boolean),
            )
          : '[]',
    }
    try {
      await api.dashboards.variable(active.id, body)
      refresh()
    } catch (error) {
      if (!active.id.startsWith('local-')) {
        toast.error(
          `Could not save variable: ${error instanceof Error ? error.message : String(error)}`,
        )
        return
      }
      setLocalDashboards((current) => {
        const next = current.map((dashboard) =>
          dashboard.id !== active.id
            ? dashboard
            : {
                ...dashboard,
                variables: [
                  ...dashboard.variables.filter((item) => item.name !== body.name),
                  { dashboard_id: active.id, ...body },
                ],
              },
        )
        saveLocalDashboards(next)
        return next
      })
    }
  }
  const removeVariable = async (name: string) => {
    if (!active) return
    if (active.id.startsWith('local-')) {
      setLocalDashboards((current) => {
        const next = current.map((d) =>
          d.id === active.id ? { ...d, variables: d.variables.filter((v) => v.name !== name) } : d,
        )
        saveLocalDashboards(next)
        return next
      })
    } else {
      await api.dashboards.deleteVariable(active.id, name)
      await refresh()
      await qc.invalidateQueries({
        queryKey: ['dashboard-runtime', active.id],
      })
      await qc.invalidateQueries({ queryKey: ['dashboard-panel', active.id] })
    }
  }
  const deleteActive = async () => {
    if (!active || !window.confirm(`Delete “${active.name}”? This cannot be undone.`)) return
    try {
      await nameSave.flush()
      if (active.id.startsWith('local-')) {
        setLocalDashboards((current) => {
          const next = current.filter((dashboard) => dashboard.id !== active.id)
          saveLocalDashboards(next)
          return next
        })
      } else {
        await api.dashboards.remove(active.id)
        await refresh()
      }
      editorNavigate('/dashboards')
    } catch (error) {
      toast.error(
        `Could not delete dashboard: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }
  const viewTextConfig = async () => {
    if (!active || active.id.startsWith('local-')) {
      setConfigError('Save this dashboard before exporting its YAML configuration.')
      setConfigText('')
      return
    }
    try {
      setConfigError(null)
      setConfigText(await api.dashboards.config(active.id))
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : String(error))
      setConfigText('')
    }
  }
  const exportAsEditableCopy = async () => {
    if (!active || !active.id.startsWith('file-')) return
    try {
      const yaml = await api.dashboards.config(active.id)
      const copy = await api.dashboards.importConfig(yaml)
      await refresh()
      toast.success('Created an editable local copy.')
      editorNavigate(`/dashboards/${copy.data.id}`)
    } catch (error) {
      toast.error(
        `Could not export editable copy: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }

  return {
    titleInputRef,
    editorTab,
    setEditorTab,
    templateId,
    setTemplateId,
    creating,
    createError,
    importing,
    importError,
    name,
    setName,
    description,
    setDescription,
    dashboardName,
    setDashboardName,
    query,
    setQuery,
    title,
    setTitle,
    display,
    setDisplay,
    settingsJSON,
    setSettingsJSON,
    layout,
    setLayout,
    editing,
    variableName,
    setVariableName,
    variableSource,
    setVariableSource,
    variableKind,
    setVariableKind,
    variableDefault,
    setVariableDefault,
    catalogSearch,
    setCatalogSearch,
    configText,
    setConfigText,
    configError,
    dashboards,
    active,
    debouncedSearch,
    catalog,
    preview,
    nameSave,
    create,
    importYAML,
    savePanel,
    createCatalogPanel,
    edit,
    movingPanel,
    movePanel,
    saveCanvasLayout,
    addVariable,
    removeVariable,
    deleteActive,
    viewTextConfig,
    exportAsEditableCopy,
  }
}
