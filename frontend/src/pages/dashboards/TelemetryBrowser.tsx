import { useState } from 'react'
import { type QueryCatalogEntry } from '@/lib/api'
import { CatalogAttributes } from '@/components/dashboard-panels/CatalogAttributes'
import { CatalogSql } from './DashboardCode'
export function TelemetryBrowser({
  search,
  setSearch,
  catalog,
  select,
  createPanel,
  loading,
  error,
}: {
  loading?: boolean
  error?: string
  search: string
  setSearch: (value: string) => void
  catalog: QueryCatalogEntry[]
  select: (item: QueryCatalogEntry) => void
  createPanel: (item: QueryCatalogEntry) => void
}) {
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const choose = (item: QueryCatalogEntry) => {
    setSelectedKey(`${item.signal}:${item.name}:${item.display_type}:${item.query}`)
    select(item)
  }
  return (
    <section>
      <header className="border-b border-border py-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Library of useful and synthetic panels</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Search examples or telemetry collected by Spaniel—metrics, spans, traces, and logs—to
              build panels.
            </p>
          </div>
          <input
            aria-label="Search telemetry SQL examples"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search examples and telemetry"
            className="w-44 rounded border border-input bg-background px-2 py-1.5 text-xs"
          />
        </div>
      </header>
      <div className="overflow-auto">
        {loading ? (
          <p role="status" className="p-4 text-xs text-muted-foreground">
            Searching examples and telemetry…
          </p>
        ) : error ? (
          <p role="alert" className="p-4 text-xs text-danger">
            Telemetry search failed: {error}
          </p>
        ) : (
          catalog.slice(0, 256).map((item) => {
            const key = `${item.signal}:${item.name}:${item.display_type}:${item.query}`
            const selected = selectedKey === key
            return (
              <div
                key={`${key}:${item.query}`}
                role="button"
                tabIndex={0}
                onClick={() => choose(item)}
                aria-pressed={selected}
                onKeyDown={(event) => {
                  if (
                    event.target === event.currentTarget &&
                    (event.key === 'Enter' || event.key === ' ')
                  ) {
                    event.preventDefault()
                    choose(item)
                  }
                }}
                className={`cursor-pointer border-b border-border px-4 py-2.5 text-left ${selected ? 'bg-accent-bg ring-1 ring-inset ring-accent' : 'hover:bg-muted'}`}
              >
                <span className="text-xs font-medium">{item.name}</span>
                <span className="ml-2 font-mono text-[9px] text-muted-foreground">
                  {item.display_type}
                </span>
                <CatalogAttributes attributes={item.attributes} />
                <CatalogSql value={item.query} />
                {selected ? (
                  <div className="mt-2 flex justify-end">
                    <button
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation()
                        createPanel(item)
                      }}
                      className="cursor-pointer rounded bg-accent px-3 py-1.5 text-xs font-medium text-accent-ink"
                    >
                      Create panel
                    </button>
                  </div>
                ) : null}
              </div>
            )
          })
        )}
        {!loading && !error && catalog.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">
            No examples or observed telemetry match this search.
          </p>
        ) : null}
      </div>
    </section>
  )
}
