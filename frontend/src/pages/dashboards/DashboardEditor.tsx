import { Link } from 'react-router-dom'
import { LayoutDashboard, Plus, Variable, Pencil } from 'lucide-react'
import { type DashboardVariable } from '@/lib/api'
import { DraftPanelEntry } from '@/components/dashboard-panels/DraftPanelEntry'
import { DashboardCanvas } from '@/components/dashboard-panels/DashboardCanvas'
import { MagicParameters } from '@/components/dashboard-panels/MagicParameters'
import { ReusableParameterList } from '@/components/dashboard-panels/ReusableParameterList'
import { NewDashboardStarter } from '@/components/dashboard-panels/NewDashboardStarter'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { panelRecipes } from './dashboard-model'
import { YamlCode } from '@/components/ui/HighlightedCode'
import { TelemetryBrowser } from './TelemetryBrowser'
import { ReadOnlyDashboardNotice } from './ReadOnlyDashboardNotice'
import { PanelStudio } from './PanelStudio'
import { useDashboardEditor } from './useDashboardEditor'
export function DashboardEditor() {
  const {
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
  } = useDashboardEditor()
  if (active) {
    const dashboard = active!
    const fileManaged = dashboard.id.startsWith('file-')
    return (
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <aside className="w-64 shrink-0 overflow-auto border-r border-border bg-surface">
          <div className="border-b border-border p-3">
            <div className="flex items-center gap-2 font-semibold">
              <LayoutDashboard size={15} /> Dashboards
            </div>
            <Link
              to="/dashboards/new"
              className="mt-3 flex w-full items-center justify-center gap-1 rounded border border-dashed border-accent bg-accent-bg px-2 py-2 text-xs font-medium text-accent-ink"
            >
              <Plus size={14} /> New dashboard
            </Link>
          </div>
          {dashboards.map((d) => (
            <Link
              key={d.id}
              to={`/dashboards/${d.id}`}
              className={`block border-b border-border px-3 py-3 ${dashboard.id === d.id ? 'bg-accent-bg' : 'hover:bg-muted'}`}
            >
              <div className="truncate text-sm font-medium">{d.name}</div>
              <div className="mt-1 font-mono text-[10px] text-muted-foreground">
                {d.panels.length} panels
              </div>
            </Link>
          ))}
        </aside>
        <main className="flex-1 overflow-auto bg-background">
          <header className="flex items-start gap-4 border-b border-border bg-surface px-6 py-5">
            <div className="min-w-0 flex-1">
              <div className="flex max-w-xl items-center gap-1">
                <input
                  ref={titleInputRef}
                  aria-label="Dashboard name"
                  value={dashboardName}
                  onChange={(event) => setDashboardName(event.target.value)}
                  disabled={fileManaged}
                  className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none disabled:cursor-default"
                />
                <button
                  type="button"
                  aria-label="Edit dashboard name"
                  disabled={fileManaged}
                  onClick={() => titleInputRef.current?.focus()}
                  className="rounded p-1.5 text-muted-foreground hover:bg-muted disabled:cursor-default disabled:opacity-40"
                >
                  <Pencil size={15} />
                </button>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                {nameSave.pending
                  ? 'Saving dashboard name…'
                  : dashboard.description || 'Query-backed telemetry views.'}
              </p>
            </div>
            <button
              onClick={() => void viewTextConfig()}
              className="rounded border border-border px-3 py-2 text-xs"
            >
              View YAML
            </button>
            {fileManaged ? (
              <button
                onClick={() => void exportAsEditableCopy()}
                className="rounded border border-accent bg-accent-bg px-3 py-2 text-xs font-medium text-accent-ink"
              >
                Export as editable copy
              </button>
            ) : (
              <button
                onClick={() => void deleteActive()}
                className="rounded border border-danger px-3 py-2 text-xs text-danger"
              >
                Delete dashboard
              </button>
            )}
          </header>
          <div className="mx-auto max-w-5xl space-y-4 p-6">
            {fileManaged && (
              <section className="rounded-md border border-amber-400/60 bg-amber-50 px-4 py-3 text-sm text-amber-950 dark:bg-amber-950/30 dark:text-amber-100">
                <strong>File-managed dashboard.</strong> YAML is authoritative and will replace UI
                edits at startup. Export an editable local copy to make changes here.
              </section>
            )}
            <Tabs value={editorTab} onValueChange={(value) => setEditorTab(String(value))}>
              <TabsList
                variant="line"
                aria-label="Panel editor"
                className="w-full justify-start border-b border-border"
              >
                <TabsTrigger value="design" className="flex-none cursor-pointer px-4">
                  Design
                </TabsTrigger>
                <TabsTrigger value="library" className="flex-none cursor-pointer px-4">
                  Library
                </TabsTrigger>
                <TabsTrigger value="panels" className="flex-none cursor-pointer px-4">
                  Panels
                </TabsTrigger>
              </TabsList>
              <TabsContent value="design" keepMounted>
                {fileManaged ? (
                  <ReadOnlyDashboardNotice copy={exportAsEditableCopy} />
                ) : (
                  <PanelStudio
                    recipe={panelRecipes.find((item) => item.type === display) ?? panelRecipes[0]}
                    title={title}
                    setTitle={setTitle}
                    display={display}
                    setDisplay={setDisplay}
                    query={query}
                    setQuery={setQuery}
                    settingsJSON={settingsJSON}
                    setSettingsJSON={setSettingsJSON}
                    layout={layout}
                    setLayout={setLayout}
                    preview={() => preview.mutate()}
                    previewState={preview}
                    save={savePanel}
                    editing={Boolean(editing)}
                  />
                )}
              </TabsContent>
              <TabsContent value="library" keepMounted>
                {fileManaged ? (
                  <ReadOnlyDashboardNotice copy={exportAsEditableCopy} />
                ) : (
                  <TelemetryBrowser
                    loading={catalog.isPending || debouncedSearch !== catalogSearch.trim()}
                    error={catalog.error?.message}
                    search={catalogSearch}
                    setSearch={setCatalogSearch}
                    catalog={catalog.data ?? []}
                    select={(item) => {
                      setTitle(item.name)
                      setQuery(item.query)
                      setDisplay(item.display_type)
                    }}
                    createPanel={createCatalogPanel}
                  />
                )}
              </TabsContent>
              <TabsContent value="panels">
                <section className="pt-3">
                  <DashboardCanvas
                    panels={dashboard.panels}
                    onEdit={fileManaged ? () => undefined : edit}
                    onCommit={fileManaged ? () => undefined : saveCanvasLayout}
                    onMove={fileManaged ? undefined : movePanel}
                    saving={fileManaged || movingPanel}
                  />
                </section>
              </TabsContent>
            </Tabs>
          </div>
        </main>
        {configText !== null && (
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Dashboard YAML configuration"
            className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"
          >
            <section className="w-full max-w-3xl overflow-hidden rounded-lg border border-border bg-background shadow-xl">
              <header className="flex items-center justify-between border-b border-border px-4 py-3">
                <div>
                  <h2 className="font-semibold">Dashboard YAML</h2>
                  <p className="text-xs text-muted-foreground">
                    Portable YAML definition; telemetry data is never included.
                  </p>
                </div>
                <button
                  onClick={() => setConfigText(null)}
                  className="rounded border border-border px-2 py-1 text-xs"
                >
                  Close
                </button>
              </header>
              <div className="max-h-[70vh] overflow-auto p-4">
                <YamlCode value={configError || configText} />
              </div>
            </section>
          </div>
        )}
        <aside className="w-[320px] shrink-0 overflow-auto border-l border-border bg-surface p-4">
          {fileManaged ? (
            <ReadOnlyDashboardNotice copy={exportAsEditableCopy} compact />
          ) : (
            <>
              <MagicParameters insert={(value) => setQuery((current) => current + value)} />
              <section className="mt-4 rounded-lg border border-border bg-background p-3">
                <h2 className="flex items-center gap-2 text-sm font-semibold">
                  <Variable size={14} /> Reusable parameters
                </h2>
                <p className="mt-1 text-xs leading-4 text-muted-foreground">
                  New dashboards include service, operation, status_code (0: unset), and severity
                  (9: INFO). Set service and operation values before using them.
                </p>
                <div className="mt-3 grid gap-2">
                  <input
                    aria-label="Variable name"
                    value={variableName}
                    onChange={(event) => setVariableName(event.target.value)}
                    placeholder="service"
                    className="w-full rounded border border-input bg-background px-2 py-1.5 text-xs"
                  />
                  <div className="grid grid-cols-2 gap-2">
                    <select
                      aria-label="Variable datatype"
                      value={variableKind}
                      onChange={(event) =>
                        setVariableKind(event.target.value as DashboardVariable['kind'])
                      }
                      className="rounded border border-input bg-background px-2 py-1.5 text-xs"
                    >
                      <option value="string">string</option>
                      <option value="number">number</option>
                      <option value="boolean">boolean</option>
                      <option value="duration">duration</option>
                      <option value="time">time range</option>
                      <option value="enum">enum</option>
                    </select>
                    <input
                      aria-label="Variable default value"
                      value={variableDefault}
                      onChange={(event) => setVariableDefault(event.target.value)}
                      placeholder="Default"
                      className="min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs"
                    />
                  </div>
                  <input
                    aria-label="Variable value source"
                    value={variableSource}
                    onChange={(event) => setVariableSource(event.target.value)}
                    placeholder="telemetry_spans.service_name"
                    className="w-full rounded border border-input bg-background px-2 py-1.5 font-mono text-xs"
                  />
                  <button
                    onClick={addVariable}
                    disabled={!variableName.trim() || !variableSource.trim()}
                    className="rounded border border-border px-2 py-1.5 text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Add parameter
                  </button>
                </div>
                <ReusableParameterList
                  variables={dashboard.variables}
                  insert={(value) => setQuery((current) => current + value)}
                  remove={removeVariable}
                />
              </section>
            </>
          )}
          <section className="mt-4 rounded-lg border border-border bg-background p-3">
            <h2 className="text-sm font-semibold">Draft canvas</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              {dashboard.panels.length} query-backed panel
              {dashboard.panels.length === 1 ? '' : 's'}
            </p>
            <div className="mt-3 space-y-2">
              {dashboard.panels.map((panel) => (
                <DraftPanelEntry
                  key={panel.id}
                  panel={panel}
                  edit={fileManaged ? () => undefined : edit}
                />
              ))}
            </div>
          </section>
        </aside>
      </div>
    )
  }
  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <aside className="w-64 shrink-0 overflow-auto border-r border-border bg-surface">
        <div className="border-b border-border p-3">
          <div className="flex items-center gap-2 font-semibold">
            <LayoutDashboard size={15} /> Dashboards
          </div>
          <Link
            to="/dashboards/new"
            className="mt-3 flex w-full cursor-pointer items-center justify-center gap-1 rounded border border-dashed border-accent bg-accent-bg px-2 py-2 text-xs font-medium text-accent-ink"
          >
            <Plus size={14} /> New dashboard
          </Link>
        </div>
        {dashboards.map((d) => (
          <Link
            key={d.id}
            to={`/dashboards/${d.id}`}
            className="block w-full cursor-pointer border-b border-border px-3 py-3 text-left hover:bg-muted"
          >
            <div className="truncate text-sm font-medium">{d.name}</div>
            <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
              {d.panels.length} panels
            </div>
          </Link>
        ))}
      </aside>
      <main className="flex-1 overflow-auto bg-background">
        <header className="border-b border-border bg-surface px-6 py-5">
          <div className="min-w-0 flex-1">
            <div className="flex max-w-xl items-center gap-1">
              <h1 className="text-xl font-semibold tracking-tight">New dashboard</h1>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              Create an editable, query-backed telemetry view.
            </p>
          </div>
        </header>
        <NewDashboardStarter
          name={name}
          setName={setName}
          description={description}
          setDescription={setDescription}
          create={() => void create()}
          templateId={templateId}
          setTemplateId={setTemplateId}
          pending={creating}
          error={createError}
          importYAML={(yaml) => void importYAML(yaml)}
          importing={importing}
          importError={importError}
        />
      </main>
      <aside className="w-[360px] shrink-0 overflow-auto border-l border-border bg-surface p-4">
        <section>
          <h2 className="text-sm font-semibold">Telemetry SQL</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Queries run read-only against the stable telemetry views. Use <code>$name</code> for a
            dashboard parameter.
          </p>
          <input
            aria-label="Search telemetry SQL examples"
            value={catalogSearch}
            onChange={(e) => setCatalogSearch(e.target.value)}
            placeholder="Search query examples"
            className="mt-3 w-full rounded border border-input bg-background px-2 py-1.5 text-xs"
          />
          {catalog.data?.map((item) => (
            <button
              key={`${item.signal}-${item.name}`}
              onClick={() => {
                setTitle(item.name)
                setQuery(item.query)
                setDisplay(item.display_type)
              }}
              className="mt-2 w-full rounded border border-border bg-background p-2 text-left hover:bg-muted"
            >
              <span className="block text-xs font-medium">{item.name}</span>
              <code className="mt-1 block truncate text-[10px] text-muted-foreground">
                {item.query}
              </code>
            </button>
          ))}
        </section>
        <section className="mt-6 border-t border-border pt-4">
          <h2 className="text-sm font-semibold">Next step</h2>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            Create or import a dashboard, then add panels, variables, and layout from its editor.
          </p>
        </section>
      </aside>
    </div>
  )
}
