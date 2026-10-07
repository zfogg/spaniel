import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, Database } from 'lucide-react'
import { api } from '@/lib/api'

export default function DatabaseSchema() {
  const [search, setSearch] = useState('')
  const catalog = useQuery({
    queryKey: ['database-schema'],
    queryFn: () => api.databaseSchema.get().then((r) => r.data),
  })
  const views = useMemo(() => {
    const q = search.toLowerCase().trim()
    return (catalog.data?.views ?? []).filter(
      (view) =>
        !q ||
        `${view.name} ${view.purpose} ${view.columns.map((c) => `${c.name} ${c.description} ${c.use_it_for}`).join(' ')}`
          .toLowerCase()
          .includes(q),
    )
  }, [catalog.data, search])
  if (catalog.isLoading)
    return (
      <main className="p-6 text-sm text-muted-foreground">Loading the running Spaniel schema…</main>
    )
  if (catalog.error)
    return (
      <main className="p-6 text-sm text-destructive">
        Could not load the schema reference: {catalog.error.message}
      </main>
    )
  const copy = (text: string) => void navigator.clipboard.writeText(text)
  return (
    <main className="flex-1 overflow-y-auto p-4 sm:p-6">
      <header className="max-w-5xl border-b border-border pb-5">
        <div className="flex items-center gap-2 text-accent-ink">
          <Database size={18} />
          <span className="font-mono text-[11px] uppercase tracking-wide">Database reference</span>
        </div>
        <h1 className="mt-2 text-xl font-semibold">Telemetry SQL, with guardrails</h1>
        <p className="mt-2 max-w-3xl text-sm text-muted-foreground">
          These are the stable, read-only telemetry views exposed by this running Spaniel binary.
          Start with a sample, then adapt it for your dashboard or alert.
        </p>
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search tables, columns, or use cases"
          className="mt-4 w-full max-w-md rounded border border-input bg-background px-3 py-2 text-sm"
        />
        <p className="mt-3 font-mono text-[10px] text-muted-foreground">
          Catalog v{catalog.data?.version} · {catalog.data?.fingerprint.slice(0, 12)} ·{' '}
          {catalog.data?.parameters.join(' · ')}
        </p>
      </header>
      <div className="mx-auto max-w-5xl space-y-8 py-6">
        {views.map((view) => (
          <section key={view.name} className="rounded-md border border-border bg-background">
            <header className="border-b border-border bg-muted/30 px-4 py-3">
              <code className="text-sm font-semibold">{view.name}</code>
              <p className="mt-1 text-sm text-muted-foreground">{view.purpose}</p>
            </header>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead className="font-mono text-[10px] uppercase text-muted-foreground">
                  <tr>
                    <th className="px-4 py-2">Column</th>
                    <th className="px-4 py-2">Type</th>
                    <th className="px-4 py-2">Use it for</th>
                  </tr>
                </thead>
                <tbody>
                  {view.columns.map((column) => (
                    <tr key={column.name} className="border-t border-border">
                      <td className="px-4 py-2 font-mono">
                        {column.name}
                        <div className="mt-0.5 font-sans text-muted-foreground">
                          {column.description}
                        </div>
                      </td>
                      <td className="px-4 py-2 font-mono text-accent-ink">{column.type}</td>
                      <td className="px-4 py-2 text-muted-foreground">{column.use_it_for}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {view.samples.length > 0 && (
              <div className="border-t border-border p-4">
                <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  Working samples
                </h2>
                <div className="grid gap-3 md:grid-cols-2">
                  {view.samples.map((sample) => (
                    <article key={sample.id} className="rounded border border-border p-3">
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <h3 className="text-sm font-medium">{sample.title}</h3>
                          <p className="mt-1 text-xs text-muted-foreground">{sample.explanation}</p>
                        </div>
                        <button
                          onClick={() => copy(sample.sql)}
                          className="rounded border border-border p-1.5 text-muted-foreground hover:bg-muted"
                          aria-label={`Copy ${sample.title}`}
                        >
                          <Copy size={13} />
                        </button>
                      </div>
                      <pre className="mt-3 overflow-x-auto rounded bg-muted p-2 text-[10px] leading-relaxed">
                        {sample.sql}
                      </pre>
                    </article>
                  ))}
                </div>
              </div>
            )}
          </section>
        ))}
      </div>
    </main>
  )
}
