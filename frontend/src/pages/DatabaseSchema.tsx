import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, Database, Search } from 'lucide-react'
import { api } from '@/lib/api'
import { SqlCode } from '@/components/ui/HighlightedCode'

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

function AttributeGuide({ viewName }: { viewName: string }) {
  if (!['telemetry_spans', 'telemetry_logs', 'telemetry_metrics'].includes(viewName)) return null
  return (
    <section className="mt-4 overflow-hidden rounded-md border border-[#d7e5ed] bg-[#fbfdff] dark:border-border dark:bg-muted">
      <header className="border-b border-[#e0eaf0] bg-[#f4f9fc] px-3 py-2 dark:border-border dark:bg-surface">
        <b className="text-[11px]">Query attributes</b>
        <span className="ml-2 font-mono text-[9px] text-[#7890a1]">
          attributes is serialized JSON
        </span>
      </header>
      <div className="p-3">
        <p className="text-[11px] leading-relaxed text-[#546d7f] dark:text-muted-foreground">
          Use a JSONPath with quoted semantic-convention keys. Spaniel suggests observed keys from
          this session; these are examples.
        </p>
        <code className="mt-2 block rounded bg-[#eaf4fa] px-2 py-2 font-mono text-[10px] text-[#315d7e] dark:bg-background dark:text-foreground">
          {`json_extract_string(attributes, '$."http.route"')`}
        </code>
        <div className="mt-2 flex flex-wrap gap-1">
          {['http.route', 'http.request.method', 'db.system', 'error.type'].map((key) => (
            <span
              key={key}
              className="rounded border border-[#cbdde8] bg-white px-2 py-1 font-mono text-[9px] text-[#426b8c] dark:bg-background"
            >
              {key}
            </span>
          ))}
        </div>
      </div>
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
                  {view.columns.length} columns · {(view.samples ?? []).length} samples
                </small>
              </button>
            ))}
          </nav>
        </aside>
        <section className="h-fit overflow-hidden rounded-lg border border-[#cbdde8] bg-white dark:border-border dark:bg-surface">
          <header className="border-b border-[#d8e5ed] bg-[#f8fbfd] px-4 py-3 dark:border-border dark:bg-muted">
            <h2 className="font-mono text-[13px] font-semibold text-[#315d7e] dark:text-[#9bd2ff]">
              {selected.name}
            </h2>
            <p className="mt-1 text-[11px] leading-relaxed text-[#627789] dark:text-muted-foreground">
              {selected.purpose}
            </p>
          </header>
          <div className="p-4">
            <AttributeGuide viewName={selected.name} />
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
                      <td className="px-2 py-2 font-mono text-[10px] font-semibold text-[#315d7e] dark:text-[#9bd2ff]">
                        {column.name}
                        <div className="mt-1 font-sans text-[10px] font-normal text-[#627789] dark:text-muted-foreground">
                          {column.description}
                        </div>
                      </td>
                      <td className="px-2 py-2 font-mono text-[10px] text-[#326348] dark:text-[#9bd8ad]">
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
            {(selected.samples ?? []).length > 0 && (
              <h3 className="mt-5 font-mono text-[11px] font-semibold uppercase tracking-wide text-[#315d7e] dark:text-[#9bd2ff]">
                Working samples
              </h3>
            )}
            {(selected.samples ?? []).map((sample) => (
              <section
                key={sample.id}
                className="mt-3 overflow-hidden rounded-md border border-[#cbdce8] bg-[#f5faff] dark:border-[#314a61] dark:bg-[#102033]"
              >
                <header className="border-b border-[#d5e5ef] bg-[#edf5fa] px-3 py-2 font-mono text-[10px] font-semibold text-[#315d7e] dark:border-[#314a61] dark:bg-[#172d43] dark:text-[#9bd2ff]">
                  {sample.title}
                </header>
                <p className="px-3 pt-2 text-[10px] text-[#627789] dark:text-muted-foreground">
                  {sample.explanation}
                </p>
                <div className="overflow-x-auto px-3 py-3">
                  <SqlCode value={sample.sql} />
                </div>
                <div className="flex gap-2 px-3 pb-3">
                  <button
                    onClick={() => copy(sample.sql)}
                    className="inline-flex items-center gap-1 rounded border border-[#b7cddd] bg-white px-2.5 py-1.5 text-[10px] font-medium text-[#315d7e] hover:bg-[#e8f3fa] dark:border-[#3d6482] dark:bg-[#183652] dark:text-[#b8e2ff] dark:hover:bg-[#214766]"
                  >
                    <Copy size={11} />
                    Copy to clipboard
                  </button>
                  <button
                    title="Use the dashboard or alert SQL editor to preview this sample in authoring context."
                    className="rounded border border-[#b7cddd] bg-white px-2.5 py-1.5 text-[10px] font-medium text-[#315d7e] hover:bg-[#e8f3fa] dark:border-[#3d6482] dark:bg-[#183652] dark:text-[#b8e2ff] dark:hover:bg-[#214766]"
                  >
                    Preview sample · 30 rows
                  </button>
                </div>
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
          <Tip title="Session and named parameters">
            Use <code>$session_id</code> to scope the active session. Named dashboard and alert
            parameters are safely bound, never interpolated into SQL.
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
