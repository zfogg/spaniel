import { PanelBuilderControls } from '@/components/dashboard-panels/PanelBuilderControls'
import { PanelPreview, type PreviewSnapshot } from '@/components/dashboard-panels/PanelPreview'
import { SchemaHelpLink } from '@/components/dashboard-panels/SchemaHelpLink'
import { SqlEditor } from '@/components/SqlCode'
import { panelRecipes } from './dashboard-model'
import { RendererSettings } from './RendererSettings'
export function PanelStudio({
  recipe,
  title,
  setTitle,
  display,
  setDisplay,
  query,
  setQuery,
  settingsJSON,
  setSettingsJSON,
  layout,
  setLayout,
  preview,
  previewState,
  save,
  editing,
}: {
  recipe: (typeof panelRecipes)[number]
  title: string
  setTitle: (value: string) => void
  display: string
  setDisplay: (value: string) => void
  query: string
  setQuery: (value: string) => void
  settingsJSON: string
  setSettingsJSON: (value: string) => void
  layout: { x: number; y: number; w: number; h: number }
  setLayout: (value: { x: number; y: number; w: number; h: number }) => void
  preview: () => void
  previewState: {
    data?: PreviewSnapshot
    isPending: boolean
    error: Error | null
  }
  save: () => void
  editing: boolean
}) {
  return (
    <section>
      <header className="border-b border-border py-3">
        <h2 className="text-[13px] font-semibold">{editing ? 'Edit panel' : 'Design a panel'}</h2>
        <p className="mt-1 text-[11px] leading-[1.4] text-muted-foreground">
          Start from the question you want answered. Spaniel explains the result shape and offers
          editable sample SQL—it does not guess your telemetry.
        </p>
      </header>
      <div className="grid grid-cols-2 gap-2 border-b border-border py-2.5 sm:grid-cols-3 lg:grid-cols-4">
        {panelRecipes.map((item) => (
          <button
            key={item.type}
            onClick={() => setDisplay(item.type)}
            className={`min-h-[68px] rounded-md border p-2 text-left text-[10px] ${display === item.type ? 'border-[#7aa3c4] bg-[#e5f0f7] text-accent-ink shadow-[inset_2px_0_0_var(--accent)] dark:bg-accent-bg' : 'border-[#d3e1ea] bg-surface text-muted-foreground hover:border-accent'}`}
          >
            <span className="block text-[11px] font-semibold text-foreground">
              <i className="mr-1 font-mono text-sm not-italic text-accent-ink">{item.icon}</i>
              {item.label}
            </span>
            <span className="mt-1 block leading-[1.25]">{item.description}</span>
          </button>
        ))}
      </div>
      <div className="grid gap-3 py-3 sm:grid-cols-[minmax(0,1.25fr)_minmax(230px,.75fr)]">
        <PanelBuilderControls key={display} display={display} apply={setQuery} />
        <aside className="rounded-md border border-border bg-background p-2.5">
          <h3 className="mb-2 text-[11px] font-semibold">Expected result</h3>
          {recipe.shape.map(([name, type]) => (
            <div
              key={name}
              className="flex justify-between border-b border-border py-1.5 font-mono text-[10px] last:border-0"
            >
              <span>{name}</span>
              <span className="text-emerald-700 dark:text-emerald-300">{type}</span>
            </div>
          ))}
          <p className="mt-3 text-[10px] text-muted-foreground">{recipe.hint}</p>
        </aside>
      </div>
      <section className="border-t border-border bg-background">
        <div className="flex w-full items-center justify-between border-b border-border bg-accent-bg px-3 py-2">
          <strong className="block whitespace-nowrap text-[11px]">Read-only DuckDB SQL</strong>
          <SchemaHelpLink />
        </div>
        <div className="grid gap-2 border-b border-border bg-muted/30 p-2.5 sm:grid-cols-2">
          <label className="font-mono text-[10px] text-muted-foreground">
            Panel name
            <input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="e.g. Checkout errors"
              className="mt-1 block w-full rounded border border-input bg-background px-2 py-1.5 font-sans text-[11px] text-foreground"
            />
          </label>
          <fieldset className="font-mono text-[10px] text-muted-foreground">
            <legend>Canvas placement</legend>
            <div className="mt-1 grid grid-cols-4 gap-1">
              {(['x', 'y', 'w', 'h'] as const).map((key) => (
                <label key={key}>
                  {key}
                  <input
                    aria-label={`Panel ${key}`}
                    type="number"
                    min="1"
                    max={key === 'w' ? 12 : key === 'h' ? 6 : 99}
                    value={layout[key]}
                    onChange={(event) =>
                      setLayout({
                        ...layout,
                        [key]: Math.max(1, Number(event.target.value) || 1),
                      })
                    }
                    className="mt-1 w-full rounded border border-input bg-background px-1 py-1 text-[11px]"
                  />
                </label>
              ))}
            </div>
          </fieldset>
          <RendererSettings display={display} value={settingsJSON} onChange={setSettingsJSON} />
          {['deploy_correlation', 'entity_list', 'time_series', 'heatmap'].includes(display) ? (
            <label className="sm:col-span-2 font-mono text-[10px] text-muted-foreground">
              Advanced renderer settings JSON
              <textarea
                aria-label="Renderer settings JSON"
                value={settingsJSON}
                onChange={(event) => setSettingsJSON(event.target.value)}
                className="mt-1 block min-h-20 w-full rounded border border-input bg-background p-2 font-mono text-[11px]"
              />
            </label>
          ) : null}
        </div>
        <div className="p-2.5">
          <SqlEditor value={query} onChange={setQuery} />
        </div>
        <div className="flex justify-end gap-2 border-t border-border px-2.5 py-2">
          <button
            type="button"
            onClick={preview}
            disabled={previewState.isPending || !query.trim()}
            className="cursor-pointer rounded border border-border bg-background px-3 py-1.5 text-[11px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          >
            {previewState.isPending ? 'Running preview…' : 'Run preview'}
          </button>
          <button
            type="button"
            onClick={save}
            className="cursor-pointer rounded bg-accent px-3 py-1.5 text-[11px] font-medium text-accent-ink"
          >
            {editing ? 'Save changes' : 'Add panel to draft'}
          </button>
        </div>
        <PanelPreview
          title={title}
          query={query}
          display={display}
          result={previewState.data}
          pending={previewState.isPending}
          error={previewState.error}
        />
      </section>
    </section>
  )
}
