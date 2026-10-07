import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, Database, Search } from 'lucide-react'
import { api } from '@/lib/api'

function Tip({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="border-b border-[#e2e9ee] px-3 py-3 last:border-0 dark:border-border">
      <h3 className="text-[11px] font-semibold text-[#315d7e] dark:text-foreground">{title}</h3>
      <p className="mt-1 text-[10px] leading-relaxed text-[#627789] dark:text-muted-foreground">
        {children}
      </p>
    </section>
  )
}

export default function DatabaseSchema() {
  const [search, setSearch] = useState('')
  const [selectedName, setSelectedName] = useState('telemetry_spans')
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
  const selected = views.find((view) => view.name === selectedName) ?? views[0]
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
  if (!selected)
    return (
      <main className="p-6 text-sm text-muted-foreground">No schema views match that search.</main>
    )
  const copy = (text: string) => void navigator.clipboard.writeText(text)
  return (
    <main className="flex-1 overflow-y-auto bg-[#f1f6f9] text-[#263b4c] dark:bg-background dark:text-foreground">
      <header className="border-b border-[#cbdde8] bg-[#edf5fa] px-5 py-5 sm:px-7 dark:border-border dark:bg-surface">
        <div className="mx-auto max-w-[1224px]">
          <div className="flex items-center gap-2 text-[#426b8c]">
            <Database size={17} />
            <span className="font-mono text-[10px]">DATABASE REFERENCE</span>
          </div>
          <h1 className="mt-2 text-[22px] font-semibold tracking-[-.035em]">
            Database schema &amp; SQL
          </h1>
          <p className="mt-1 max-w-2xl text-[12px] leading-relaxed text-[#627789] dark:text-muted-foreground">
            Generated from Spaniel’s versioned schema catalog. Browse stable telemetry views,
            inspect columns, then copy a small working starting point into your editor.
          </p>
        </div>
      </header>
      <div className="mx-auto grid max-w-[1280px] gap-4 p-4 sm:grid-cols-[215px_minmax(0,1fr)] sm:p-7 xl:grid-cols-[215px_minmax(430px,1fr)_270px]">
        <aside className="h-fit overflow-hidden rounded-lg border border-[#cbdde8] bg-white dark:border-border dark:bg-surface">
          <header className="border-b border-[#d8e5ed] bg-[#f8fbfd] px-3 py-3 dark:border-border dark:bg-muted">
            <b className="text-[12px]">Telemetry views</b>
            <p className="mt-1 font-mono text-[9px] text-[#7890a1]">Stable query surfaces</p>
          </header>
          <label className="relative block p-3">
            <Search className="absolute left-5 top-[22px] text-[#7890a1]" size={13} />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Find a view or column"
              className="w-full rounded-[5px] border border-[#cbdde8] bg-white py-1.5 pl-7 pr-2 text-[11px] outline-none focus:border-[#7aa3c4] dark:bg-background"
            />
          </label>
          <nav>
            {views.map((view) => (
              <button
                key={view.name}
                onClick={() => setSelectedName(view.name)}
                className={`block w-full border-t border-[#e2e9ee] px-3 py-2.5 text-left text-[11px] transition-colors ${view.name === selected.name ? 'border-l-[3px] border-l-[#7aa3c4] bg-[#e8f3fa] font-semibold text-[#315d7e]' : 'text-[#536b7c] hover:border-l-[3px] hover:border-l-[#7aa3c4] hover:bg-[#f3f8fb]'} dark:border-border dark:text-muted-foreground dark:hover:bg-muted`}
              >
                <span className="font-mono">{view.name}</span>
                <small className="mt-1 block font-mono text-[9px] font-normal text-[#7890a1]">
                  {view.columns.length} columns · {view.samples.length} samples
                </small>
              </button>
            ))}
          </nav>
        </aside>
        <section className="h-fit overflow-hidden rounded-lg border border-[#cbdde8] bg-white dark:border-border dark:bg-surface">
          <header className="border-b border-[#d8e5ed] bg-[#f8fbfd] px-4 py-3 dark:border-border dark:bg-muted">
            <h2 className="font-mono text-[13px] font-semibold text-[#315d7e] dark:text-accent-ink">
              {selected.name}
            </h2>
            <p className="mt-1 text-[11px] leading-relaxed text-[#627789] dark:text-muted-foreground">
              {selected.purpose}
            </p>
          </header>
          <div className="p-4">
            <div className="border-l-[3px] border-[#7aa3c4] bg-[#eef6fb] px-3 py-2 text-[11px] leading-relaxed text-[#546d7f] dark:bg-muted dark:text-muted-foreground">
              Use <code>$session_id</code> to scope the active session. Named dashboard and alert
              parameters are safely bound, never interpolated into SQL.
            </div>
            <div className="mt-4 overflow-x-auto">
              <table className="w-full text-left">
                <thead className="border-b border-[#d8e5ed] font-mono text-[9px] text-[#7890a1]">
                  <tr>
                    <th className="px-2 py-2 font-medium">COLUMN</th>
                    <th className="px-2 py-2 font-medium">TYPE</th>
                    <th className="px-2 py-2 font-medium">USE IT FOR</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.columns.map((column) => (
                    <tr
                      key={column.name}
                      className="border-b border-[#e2e9ee] align-top dark:border-border"
                    >
                      <td className="px-2 py-2 font-mono text-[10px] font-semibold text-[#315d7e]">
                        {column.name}
                        <div className="mt-1 font-sans text-[10px] font-normal text-[#627789] dark:text-muted-foreground">
                          {column.description}
                        </div>
                      </td>
                      <td className="px-2 py-2 font-mono text-[10px] text-[#326348]">
                        {column.type}
                      </td>
                      <td className="px-2 py-2 text-[11px] leading-relaxed text-[#546d7f] dark:text-muted-foreground">
                        {column.use_it_for}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {selected.samples.map((sample) => (
              <section
                key={sample.id}
                className="mt-4 overflow-hidden rounded-md border border-[#cbdce8] bg-[#f5faff] dark:border-border dark:bg-muted"
              >
                <header className="flex items-center justify-between border-b border-[#d5e5ef] bg-[#edf5fa] px-3 py-2 font-mono text-[10px] text-[#315d7e] dark:border-border dark:bg-surface">
                  <span>Working sample · {sample.title}</span>
                  <button
                    onClick={() => copy(sample.sql)}
                    className="inline-flex items-center gap-1 rounded border border-[#b7cddd] bg-white px-2 py-1 text-[9px] hover:bg-[#e8f3fa] dark:bg-background"
                  >
                    <Copy size={11} />
                    Copy
                  </button>
                </header>
                <p className="px-3 pt-2 text-[10px] text-[#627789] dark:text-muted-foreground">
                  {sample.explanation}
                </p>
                <pre className="overflow-x-auto px-3 py-3 font-mono text-[10px] leading-relaxed text-[#29475f] dark:text-foreground">
                  {sample.sql}
                </pre>
              </section>
            ))}
          </div>
        </section>
        <aside className="h-fit overflow-hidden rounded-lg border border-[#cbdde8] bg-white xl:sticky xl:top-4 dark:border-border dark:bg-surface">
          <header className="border-b border-[#d8e5ed] bg-[#f8fbfd] px-3 py-3 dark:border-border dark:bg-muted">
            <h3 className="text-[12px] font-semibold">SQL guardrails</h3>
            <p className="mt-1 font-mono text-[9px] text-[#7890a1]">
              Short rules that matter while authoring.
            </p>
          </header>
          <Tip title="Only read-only SQL">
            Spaniel permits one query statement and blocks writes, extensions, and multi-statement
            input.
          </Tip>
          <Tip title="Time is nanoseconds">
            Use <code>make_timestamp_ns(start_ns)</code> for readable time. A time-series result
            needs a timestamp and <code>value</code>.
          </Tip>
          <Tip title="Result shape drives display">
            Keep the aliases expected by your chosen panel renderer when you adapt a sample.
          </Tip>
          <div className="border-t border-[#d8e5ed] bg-[#f8fbfd] px-3 py-3 font-mono text-[9px] text-[#7890a1] dark:border-border dark:bg-muted">
            Catalog v{catalog.data?.version} · {catalog.data?.fingerprint.slice(0, 12)}
          </div>
        </aside>
      </div>
    </main>
  )
}
