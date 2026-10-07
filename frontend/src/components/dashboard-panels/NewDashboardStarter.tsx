import { dashboardTemplates } from './dashboard-templates'

export function NewDashboardStarter({
  name,
  setName,
  description,
  setDescription,
  templateId,
  setTemplateId,
  create,
  pending,
  error,
}: {
  name: string
  setName: (value: string) => void
  description: string
  setDescription: (value: string) => void
  templateId: string
  setTemplateId: (value: string) => void
  create: () => void
  pending: boolean
  error: string
}) {
  const selected =
    dashboardTemplates.find((item) => item.id === templateId) ?? dashboardTemplates[0]
  return (
    <div className="dashboard-new-layout mx-auto max-w-2xl space-y-4 p-6">
      <section className="rounded-lg border border-border bg-surface">
        <header className="border-b border-border px-4 py-3">
          <h2 id="template-heading" className="text-sm font-semibold">
            Choose a starting point
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Start empty or choose a team dashboard. Every panel uses your telemetry and stays
            editable.
          </p>
        </header>
        <fieldset
          disabled={pending}
          aria-labelledby="template-heading"
          className="grid grid-cols-2 gap-2 p-3"
        >
          {dashboardTemplates.map((template) => (
            <label
              key={template.id}
              className={`cursor-pointer rounded border p-3 text-left focus-within:ring-2 focus-within:ring-ring ${templateId === template.id ? 'border-accent bg-accent-bg text-accent-ink' : 'border-border bg-background hover:border-accent hover:bg-muted'}`}
            >
              <span className="flex items-center justify-between">
                <span aria-hidden="true" className="font-mono text-lg">
                  {template.icon}
                </span>
                <input
                  type="radio"
                  name="dashboard-template"
                  aria-label={template.name}
                  value={template.id}
                  checked={templateId === template.id}
                  onChange={() => setTemplateId(template.id)}
                  className="cursor-pointer accent-[var(--accent-ink)]"
                />
              </span>
              <span className="mt-2 block text-xs font-medium">{template.name}</span>
              <span className="mt-1 block text-[11px] leading-4">{template.description}</span>
              <span className="mt-2 block text-[10px] text-muted-foreground">
                {template.panels.length
                  ? `${template.panels.length} panels · last hour`
                  : 'No panels'}
              </span>
            </label>
          ))}
        </fieldset>
        {selected.panels.length > 0 && (
          <div className="border-t border-border px-4 py-3" aria-live="polite">
            <h3 className="text-xs font-semibold">Inside {selected.name}</h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              {selected.needs} Missing signals show an empty panel, never synthetic readings.
            </p>
            <ul className="mt-2 divide-y divide-border">
              {selected.panels.map((panel) => (
                <li
                  key={panel.title}
                  className="flex items-baseline justify-between gap-3 py-1.5 text-xs"
                >
                  <span>{panel.title}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">
                    {panel.display_type.replace(/_/g, ' ')}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          if (!pending) create()
        }}
        className="rounded-lg border border-border bg-surface"
      >
        <header className="border-b border-border px-4 py-3">
          <h1 className="text-sm font-semibold">Dashboard name</h1>
          <p className="mt-1 text-xs text-muted-foreground">
            {selected.panels.length
              ? `Create ${selected.panels.length} editable panels from ${selected.name}.`
              : 'Create an empty dashboard and add your own panels.'}
          </p>
        </header>
        <fieldset disabled={pending} className="p-4">
          <label className="block text-xs font-medium">
            Name
            <input
              value={name}
              maxLength={120}
              onChange={(event) => setName(event.target.value)}
              placeholder={selected.id === 'none' ? 'New dashboard' : selected.name}
              className="mt-1.5 w-full rounded border border-input bg-background px-2.5 py-2 text-sm"
            />
          </label>
          <label className="mt-3 block text-xs font-medium">
            Description <span className="font-normal text-muted-foreground">optional</span>
            <textarea
              value={description}
              maxLength={1000}
              onChange={(event) => setDescription(event.target.value)}
              placeholder={
                selected.id === 'none'
                  ? 'What will this dashboard help you see?'
                  : selected.description
              }
              rows={2}
              className="mt-1.5 w-full rounded border border-input bg-background px-2.5 py-2 text-sm"
            />
          </label>
          {error && (
            <p role="alert" className="mt-3 text-xs text-danger">
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={pending}
            className="mt-4 cursor-pointer rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink disabled:cursor-wait disabled:opacity-60"
          >
            {pending ? 'Creating dashboard…' : 'Create dashboard'}
          </button>
        </fieldset>
      </form>
    </div>
  )
}
