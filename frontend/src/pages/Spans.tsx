import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  useReactTable, getCoreRowModel, getFilteredRowModel,
  type ColumnDef, type ColumnFiltersState, type Row,
} from '@tanstack/react-table'
import { qk } from '@/lib/query'
import { api, type SpanRow } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import { KIND_LABELS } from '@/lib/span-utils'
import EmptyState from '@/components/EmptyState'
import JsonView from '@/components/JsonView'

// Global search: matches a span on its name, service, trace/span id, or any
// attribute key/value. Used as the react-table globalFilterFn.
function spanGlobalFilter(row: Row<SpanRow>, _columnId: string, filterValue: string): boolean {
  const q = String(filterValue).trim().toLowerCase()
  if (!q) return true
  const s = row.original
  const attrs = parseAttrs(s.attributes)
  const hay = [
    s.name, s.service_name, s.trace_id, s.span_id,
    ...Object.keys(attrs), ...Object.values(attrs).map(String),
  ].join(' ').toLowerCase()
  return hay.includes(q)
}

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtNs(ns: number): string {
  if (ns >= 1_000_000_000) return (ns / 1_000_000_000).toFixed(2) + 's'
  if (ns >= 1_000_000) return (ns / 1_000_000).toFixed(1) + 'ms'
  if (ns >= 1_000) return (ns / 1_000).toFixed(0) + 'µs'
  return ns + 'ns'
}

function fmtTime(ns: number): string {
  const d = new Date(ns / 1_000_000)
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  const ms = d.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}

function parseAttrs(raw: string): Record<string, unknown> {
  try { return JSON.parse(raw) } catch { return {} }
}

const SLOW_NS = 250_000_000

// ── SvcChip ───────────────────────────────────────────────────────────────────

function SvcChip({ name }: { name: string }) {
  const { fg, bg } = svcColor(name)
  return (
    <span
      className="inline-flex items-center gap-[5px] px-[7px] py-0.5 rounded-[4px] font-mono text-[10.5px] font-medium whitespace-nowrap overflow-hidden text-ellipsis max-w-full border"
      style={{
        color: fg, background: bg,
        borderColor: fg + '44',
      }}
    >
      {name}
    </span>
  )
}

// ── TagBadge ──────────────────────────────────────────────────────────────────

function TagBadge({ tag }: { tag?: string }) {
  if (!tag) return null
  // Use the Drift *-bg + *-ink token pairs (light: tinted bg, darker ink;
  // dark: deep bg, lifted ink). The previous `color: #fff` on `--danger`
  // (#d68a7a in light mode) produced white-on-pale-coral — unreadable.
  const tones: Record<string, string> = {
    'n+1':  'bg-[var(--danger-bg)] text-[var(--danger-ink)]',
    error:  'bg-[var(--danger-bg)] text-[var(--danger-ink)]',
    slow:   'bg-[var(--warn-bg)]   text-[var(--warn-ink)]',
    lint:   'bg-[var(--warn-bg)]   text-[var(--warn-ink)]',
  }
  const tone = tones[tag] ?? 'bg-muted text-muted-foreground'
  return (
    <span className={`inline-block rounded-[4px] px-1.5 py-px font-mono text-[10px] font-semibold ${tone}`}>
      {tag}
    </span>
  )
}

// ── FilterChip ────────────────────────────────────────────────────────────────

function FilterChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <span
      className="inline-flex items-center gap-[5px] pl-2.5 pr-2 py-1 rounded-[14px] font-mono text-[11px] font-medium text-foreground border"
      style={{
        background: 'color-mix(in oklch, var(--accent) 14%, var(--background))',
        borderColor: 'color-mix(in oklch, var(--accent) 30%, transparent)',
      }}
    >
      {label}
      <button
        type="button"
        onClick={onClear}
        className="bg-transparent border-none cursor-pointer text-inherit text-sm leading-none p-0 ml-0.5"
      >×</button>
    </span>
  )
}

// ── SidebarGroup ──────────────────────────────────────────────────────────────

function SbGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-5">
      <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] px-3 mb-1">{title}</div>
      {children}
    </div>
  )
}

function SbItem({
  active, dot, count, onClick, children,
}: {
  active: boolean; dot?: string; count: number; onClick: () => void; children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex items-center gap-[7px] w-full text-left px-3 py-[5px] border-none cursor-pointer font-mono text-[11px] transition-[background] duration-100 border-l-2 ${active ? 'bg-muted border-l-accent-d text-foreground' : 'bg-transparent border-l-transparent text-muted-foreground'}`}
    >
      {dot && (
        <span className="w-[7px] h-[7px] rounded-[7px] shrink-0" style={{ background: dot }} />
      )}
      <span className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
        {children}
      </span>
      <span className="text-muted-foreground text-[10px] shrink-0">{count}</span>
    </button>
  )
}

// ── SpanInspector ─────────────────────────────────────────────────────────────

function SpanInspector({ span, onClose }: { span: SpanRow; onClose: () => void }) {
  const navigate = useNavigate()
  const attrs = parseAttrs(span.attributes)
  const entries = Object.entries(attrs)
  const kindLabel = KIND_LABELS[span.kind] ?? 'unknown'
  const isSlow = span.duration_ns > SLOW_NS

  return (
    <aside data-testid="span-inspector" className="w-[380px] border-l border-border bg-background flex flex-col overflow-hidden shrink-0">
      <div className="px-4 py-3.5 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <SvcChip name={span.service_name} />
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">{kindLabel}</span>
          <div className="flex-1" />
          <TagBadge tag={span.tag} />
          <button
            type="button"
            onClick={onClose}
            className="bg-transparent border-none cursor-pointer text-muted-foreground text-base leading-none p-0"
          >×</button>
        </div>
        <div className="font-mono text-[13px] text-foreground leading-[1.4] break-words">{span.name}</div>
        <div className="grid grid-cols-2 gap-3 mt-3">
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">duration</div>
            <div
              className="text-[22px] font-semibold leading-none mt-[3px]"
              style={{
                fontFamily: 'Georgia, serif',
                color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)',
              }}
            >{fmtNs(span.duration_ns)}</div>
          </div>
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">started</div>
            <div className="font-mono text-[13px] text-foreground mt-1.5">{fmtTime(span.start_ns)}</div>
          </div>
        </div>
      </div>

      <div className="px-4 py-3.5 border-b border-border bg-muted">
        <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-[5px]">belongs to trace</div>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs font-semibold text-foreground overflow-hidden text-ellipsis whitespace-nowrap">{span.trace_id}</span>
        </div>
        <div className="font-mono text-[10px] text-muted-foreground mt-0.5">
          <button
            type="button"
            onClick={() => navigate(`/traces/${span.trace_id}`)}
            className="bg-transparent border-none cursor-pointer p-0 font-mono text-[10px] underline decoration-dotted"
            style={{ color: 'var(--accent, #6366f1)' }}
          >open waterfall</button>
        </div>
      </div>

      <div className="flex-1 overflow-x-hidden overflow-y-auto">
        <div className="pt-3 px-4 pb-1 font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] flex items-baseline">
          attributes
          <span className="flex-1" />
          <span className="text-muted-foreground normal-case tracking-normal">
            {entries.length} keys
          </span>
        </div>
        <div className="pt-1 pb-3.5">
          {entries.length > 0 ? (
            <JsonView data={attrs} />
          ) : (
            <div className="px-4 py-3 font-mono text-[10.5px] text-muted-foreground">no attributes</div>
          )}
        </div>
      </div>
    </aside>
  )
}

// ── Spans page ────────────────────────────────────────────────────────────────

export default function Spans() {
  const [sortBy, setSortBy] = useState<'time' | 'dur' | 'name'>('time')
  const [query, setQuery] = useState('')
  const [svcSel, setSvcSel] = useState<string | null>(null)
  const [kindSel, setKindSel] = useState<string | null>(null)
  const [tagSel, setTagSel] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // sort is part of the server request, so it goes in the query key; live span
  // events refresh this via useLiveInvalidation() in App.tsx.
  const { data: spans = [], isLoading: loading } = useQuery({
    queryKey: qk.spans({ sort: sortBy }),
    queryFn: () => api.spans.list({ sort: sortBy }).then(r => r.data),
  })

  // facet counts computed from the full dataset
  const svcFacets = useMemo(() => {
    const m: Record<string, number> = {}
    for (const s of spans) m[s.service_name] = (m[s.service_name] ?? 0) + 1
    return Object.entries(m).sort((a, b) => b[1] - a[1])
  }, [spans])

  const kindFacets = useMemo(() => {
    const m: Record<string, number> = {}
    for (const s of spans) {
      const k = KIND_LABELS[s.kind] ?? 'unknown'
      m[k] = (m[k] ?? 0) + 1
    }
    return Object.entries(m).sort((a, b) => b[1] - a[1])
  }, [spans])

  const calloutCounts = useMemo(() => {
    const c = { 'n+1': 0, error: 0, slow: 0, lint: 0 }
    for (const s of spans) {
      if (s.tag === 'n+1') c['n+1']++
      else if (s.tag === 'error') c.error++
      else if (s.tag === 'slow') c.slow++
      else if (s.tag === 'lint') c.lint++
    }
    return c
  }, [spans])

  // Headless react-table owns the multi-filter (service/kind/tag column filters
  // + global search). Sorting stays server-side (the sort dropdown drives the
  // query key) because the backend's ORDER BY + 500-row cap means "duration"
  // returns the slowest overall, not just the slowest of the loaded page.
  const columns = useMemo<ColumnDef<SpanRow>[]>(() => [
    { id: 'service', accessorFn: s => s.service_name, filterFn: 'equals' },
    { id: 'kind', accessorFn: s => KIND_LABELS[s.kind] ?? 'unknown', filterFn: 'equals' },
    { id: 'tag', accessorFn: s => s.tag ?? '', filterFn: 'equals' },
  ], [])

  const columnFilters = useMemo<ColumnFiltersState>(() => {
    const f: ColumnFiltersState = []
    if (svcSel) f.push({ id: 'service', value: svcSel })
    if (kindSel) f.push({ id: 'kind', value: kindSel })
    if (tagSel) f.push({ id: 'tag', value: tagSel })
    return f
  }, [svcSel, kindSel, tagSel])

  const table = useReactTable({
    data: spans,
    columns,
    state: { globalFilter: query, columnFilters },
    onGlobalFilterChange: setQuery,
    onColumnFiltersChange: () => {}, // filters are driven by the sidebar selections
    globalFilterFn: spanGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    manualSorting: true,
  })

  const filtered = table.getRowModel().rows.map(r => r.original)
  const selected = filtered.find(s => s.span_id === selectedId) ?? null

  const activeFilters: { label: string; clear: () => void }[] = []
  if (svcSel) activeFilters.push({ label: `svc: ${svcSel}`, clear: () => setSvcSel(null) })
  if (kindSel) activeFilters.push({ label: `kind: ${kindSel}`, clear: () => setKindSel(null) })
  if (tagSel) activeFilters.push({ label: `tag: ${tagSel}`, clear: () => setTagSel(null) })

  return (
    <div className="flex h-full overflow-hidden">
      {/* Sidebar */}
      <aside
        data-testid="spans-sidebar"
        className="w-[200px] border-r border-border bg-background flex flex-col overflow-x-hidden overflow-y-auto shrink-0 py-3"
      >
        <SbGroup title="service">
          <SbItem active={!svcSel} onClick={() => setSvcSel(null)} count={spans.length}>
            all services
          </SbItem>
          {svcFacets.map(([svc, n]) => (
            <SbItem
              key={svc}
              active={svcSel === svc}
              dot={svcColor(svc).fg}
              count={n}
              onClick={() => setSvcSel(svcSel === svc ? null : svc)}
            >{svc}</SbItem>
          ))}
        </SbGroup>

        <SbGroup title="kind">
          {kindFacets.map(([k, n]) => (
            <SbItem
              key={k}
              active={kindSel === k}
              count={n}
              onClick={() => setKindSel(kindSel === k ? null : k)}
            >{k}</SbItem>
          ))}
        </SbGroup>

        <SbGroup title="callouts">
          <SbItem
            active={tagSel === 'n+1'}
            dot="var(--destructive, #c0392b)"
            count={calloutCounts['n+1']}
            onClick={() => setTagSel(tagSel === 'n+1' ? null : 'n+1')}
          >n+1</SbItem>
          <SbItem
            active={tagSel === 'slow'}
            dot="var(--warn, #d97706)"
            count={calloutCounts.slow}
            onClick={() => setTagSel(tagSel === 'slow' ? null : 'slow')}
          >slow (&gt;250ms)</SbItem>
          <SbItem
            active={tagSel === 'lint'}
            dot="var(--warn, #d97706)"
            count={calloutCounts.lint}
            onClick={() => setTagSel(tagSel === 'lint' ? null : 'lint')}
          >lint warnings</SbItem>
          <SbItem
            active={tagSel === 'error'}
            dot="var(--destructive, #c0392b)"
            count={calloutCounts.error}
            onClick={() => setTagSel(tagSel === 'error' ? null : 'error')}
          >errored</SbItem>
        </SbGroup>
      </aside>

      {/* Main */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden bg-background">
        {/* Search + filters bar */}
        <div className="px-3.5 py-2.5 border-b border-border flex items-center gap-2.5 flex-wrap">
          <div className="inline-flex items-center gap-[7px] bg-muted border border-border rounded-lg px-2.5 h-[30px] flex-1 min-w-[240px]">
            <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
              <circle cx="6" cy="6" r="4" stroke="var(--muted-foreground)" strokeWidth="1.4" />
              <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="var(--muted-foreground)"
                strokeWidth="1.4" strokeLinecap="round" />
            </svg>
            <input
              data-testid="spans-search"
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="search by name, attribute key or value, trace id…"
              className="flex-1 border-none outline-none bg-transparent font-mono text-xs text-foreground"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="bg-transparent border-none cursor-pointer font-mono text-muted-foreground text-[11px] p-0"
              >clear</button>
            )}
          </div>

          {activeFilters.map(f => (
            <FilterChip key={f.label} label={f.label} onClear={f.clear} />
          ))}

          <div className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground px-2.5 h-[30px] rounded-md bg-muted border border-border">
            <span>sort</span>
            <select
              data-testid="spans-sort"
              value={sortBy}
              onChange={e => setSortBy(e.target.value as typeof sortBy)}
              className="border-none outline-none bg-transparent font-mono text-[11px] text-foreground cursor-pointer"
            >
              <option value="time">time ↓</option>
              <option value="dur">duration ↓</option>
              <option value="name">name a→z</option>
            </select>
          </div>
        </div>

        {/* Column headers */}
        <div
          className="grid gap-2.5 px-3.5 py-[7px] border-b border-border font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] bg-muted"
          style={{ gridTemplateColumns: 'minmax(0,1.6fr) 130px 80px 70px 130px 60px' }}
        >
          <div>name</div>
          <div>service</div>
          <div>kind</div>
          <div className="text-right">dur</div>
          <div>trace</div>
          <div className="text-right">tag</div>
        </div>

        {/* Rows */}
        <div className="flex-1 overflow-x-hidden overflow-y-auto">
          {loading ? (
            <div className="px-5 py-10 text-center font-mono text-[11px] text-muted-foreground">loading…</div>
          ) : filtered.length === 0 ? (
            spans.length === 0 ? (
              <EmptyState
                title="No spans recorded"
                hint="Send any OTLP span — they'll appear here."
                glyph={
                  <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                    <rect x="4" y="13" width="18" height="4" rx="2" stroke="currentColor" strokeWidth="1.5" opacity="0.6" />
                    <rect x="10" y="21" width="14" height="4" rx="2" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
                    <rect x="7" y="5" width="10" height="4" rx="2" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
                  </svg>
                }
                cta={{ label: 'OTLP docs →', href: 'https://opentelemetry.io/docs/specs/otel/protocol/' }}
              />
            ) : (
              <div className="px-5 py-10 text-center font-mono text-[11px] text-muted-foreground">no spans match — clear a filter or try a different query</div>
            )
          ) : filtered.map(s => {
            const isSel = s.span_id === selectedId
            const isSlow = s.duration_ns > SLOW_NS
            const kindLabel = KIND_LABELS[s.kind] ?? 'unknown'
            return (
              <button
                key={s.span_id}
                type="button"
                data-testid={`span-row-${s.span_id}`}
                onClick={() => setSelectedId(isSel ? null : s.span_id)}
                className={`grid text-left cursor-pointer gap-2.5 px-3.5 py-2 items-center border-none w-full border-b border-border outline-none border-l-2 ${isSel ? 'border-l-[var(--accent,#6366f1)]' : 'border-l-transparent bg-transparent'}`}
                style={{
                  gridTemplateColumns: 'minmax(0,1.6fr) 130px 80px 70px 130px 60px',
                  background: isSel
                    ? 'color-mix(in oklch, var(--accent, #6366f1) 14%, var(--background))'
                    : undefined,
                }}
              >
                <div className="font-mono text-[11.5px] text-foreground overflow-hidden text-ellipsis whitespace-nowrap">{s.name}</div>
                <div><SvcChip name={s.service_name} /></div>
                <div className="font-mono text-[10px] text-muted-foreground uppercase tracking-[0.08em]">{kindLabel}</div>
                <div
                  className="text-right font-mono text-[11.5px] font-semibold"
                  style={{ color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)' }}
                >{fmtNs(s.duration_ns)}</div>
                <div className="font-mono text-[10px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap">{s.trace_id}</div>
                <div className="text-right">
                  <TagBadge tag={s.tag} />
                </div>
              </button>
            )
          })}
        </div>

        {/* Status footer */}
        <div className="px-3.5 py-2 border-t border-border bg-muted font-mono text-[10.5px] text-muted-foreground flex items-center gap-3.5">
          <span>
            <strong className="text-foreground">{filtered.length.toLocaleString()}</strong>
            {' '}of {spans.length.toLocaleString()} spans
          </span>
          <span>·</span>
          <span>last 24h</span>
          <span className="flex-1" />
          <code className="px-2 py-[3px] rounded-[5px] border border-border bg-background text-muted-foreground font-mono text-[10px]">spaniel spans --tail</code>
        </div>
      </div>

      {/* Inspector */}
      {selected && (
        <SpanInspector span={selected} onClose={() => setSelectedId(null)} />
      )}
    </div>
  )
}
