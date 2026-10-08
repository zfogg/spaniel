import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  type ColumnDef,
  type ColumnFiltersState,
  type Row,
} from '@tanstack/react-table'
import { qk } from '@/lib/query'
import { api, type SpanGroup, type SpanRow } from '@/lib/api'
import { svcColor, httpDisplayName } from '@/lib/span-utils'
import { KIND_LABELS } from '@/lib/span-utils'
import EmptyState from '@/components/EmptyState'
import ErrorState from '@/components/ErrorState'
import JsonView from '@/components/JsonView'
import PaginationControls from '@/components/PaginationControls'
import { fmtDuration, fmtClock } from '@/lib/fmt-relative'
import { AnimatePresence, motion } from 'motion/react'
import { TelemetryArrival } from '@/components/TelemetryArrival'
import { useNewItemIDs } from '@/lib/use-new-item-ids'

// Global search: matches a span on its name, service, trace/span id, or any
// attribute key/value. Used as the react-table globalFilterFn.
function spanGlobalFilter(row: Row<SpanRow>, _columnId: string, filterValue: string): boolean {
  const q = String(filterValue).trim().toLowerCase()
  if (!q) return true
  const s = row.original
  const attrs = parseAttrs(s.attributes)
  const hay = [
    s.name,
    s.service_name,
    s.trace_id,
    s.span_id,
    ...Object.keys(attrs),
    ...Object.values(attrs).map(String),
  ]
    .join(' ')
    .toLowerCase()
  return hay.includes(q)
}

// ── helpers ───────────────────────────────────────────────────────────────────

function parseAttrs(raw: string): Record<string, unknown> {
  try {
    return JSON.parse(raw)
  } catch {
    return {}
  }
}

const SLOW_NS = 250_000_000
const PAGE_SIZE = 100
type SpanView = 'all' | 'grouped'
type SpanSort = 'time' | 'dur' | 'name'

const SPAN_SORTS: Array<{ value: SpanSort; label: string }> = [
  { value: 'time', label: 'time ↓' },
  { value: 'dur', label: 'duration ↓' },
  { value: 'name', label: 'name a→z' },
]

type SpanDrilldown = Pick<SpanGroup, 'service_name' | 'name' | 'kind'>

function drilldownFromURL(params: URLSearchParams): SpanDrilldown | null {
  const service_name = params.get('groupService')
  const name = params.get('groupName')
  const kind = Number(params.get('groupKind'))
  if (!service_name || !name || !Number.isInteger(kind)) return null
  return { service_name, name, kind }
}

// ── SvcChip ───────────────────────────────────────────────────────────────────

function SvcChip({ name }: { name: string }) {
  const { fg, bg } = svcColor(name)
  return (
    <span
      className="inline-flex items-center gap-[5px] px-[7px] py-0.5 rounded-[4px] font-mono text-[10.5px] font-medium whitespace-nowrap overflow-hidden text-ellipsis max-w-full border"
      style={{
        color: fg,
        background: bg,
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
    'n+1': 'bg-[var(--danger-bg)] text-[var(--danger-ink)]',
    error: 'bg-[var(--danger-bg)] text-[var(--danger-ink)]',
    slow: 'bg-[var(--warn-bg)]   text-[var(--warn-ink)]',
    lint: 'bg-[var(--warn-bg)]   text-[var(--warn-ink)]',
  }
  const tone = tones[tag] ?? 'bg-muted text-muted-foreground'
  return (
    <span
      className={`inline-block rounded-[4px] px-1.5 py-px font-mono text-[10px] font-semibold ${tone}`}
    >
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
        aria-label="Clear filter"
        className="bg-transparent border-none cursor-pointer text-inherit text-sm leading-none p-0 ml-0.5"
      >
        ×
      </button>
    </span>
  )
}

// ── SidebarGroup ──────────────────────────────────────────────────────────────

function SbGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-5">
      <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] px-3 mb-1">
        {title}
      </div>
      {children}
    </div>
  )
}

function SbItem({
  active,
  dot,
  count,
  onClick,
  children,
}: {
  active: boolean
  dot?: string
  count: number
  onClick: () => void
  children: React.ReactNode
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
      <span className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap">{children}</span>
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
    <aside
      data-testid="span-inspector"
      className="w-[380px] border-l border-border bg-background flex flex-col overflow-hidden shrink-0"
    >
      <div className="px-4 py-3.5 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <SvcChip name={span.service_name} />
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">
            {kindLabel}
          </span>
          <div className="flex-1" />
          <TagBadge tag={span.tag} />
          <button
            type="button"
            onClick={onClose}
            aria-label="Close inspector"
            className="bg-transparent border-none cursor-pointer text-muted-foreground text-base leading-none p-0"
          >
            ×
          </button>
        </div>
        <div className="font-mono text-[13px] text-foreground leading-[1.4] break-words">
          {httpDisplayName(span)}
        </div>
        <div className="grid grid-cols-2 gap-3 mt-3">
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">
              duration
            </div>
            <div
              className="text-[22px] font-semibold leading-none mt-[3px]"
              style={{
                fontFamily: 'Georgia, serif',
                color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)',
              }}
            >
              {fmtDuration(span.duration_ns)}
            </div>
          </div>
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">
              started
            </div>
            <div className="font-mono text-[13px] text-foreground mt-1.5">
              {fmtClock(span.start_ns)}
            </div>
          </div>
        </div>
      </div>

      <div className="px-4 py-3.5 border-b border-border bg-muted">
        <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-[5px]">
          belongs to trace
        </div>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs font-semibold text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
            {span.trace_id}
          </span>
        </div>
        <div className="font-mono text-[10px] text-muted-foreground mt-0.5">
          <button
            type="button"
            onClick={() => navigate(`/traces/${span.trace_id}`)}
            className="bg-transparent border-none cursor-pointer p-0 font-mono text-[10px] underline decoration-dotted"
            style={{ color: 'var(--accent, #6366f1)' }}
          >
            open waterfall
          </button>
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
            <div className="px-4 py-3 font-mono text-[10.5px] text-muted-foreground">
              no attributes
            </div>
          )}
        </div>
      </div>
    </aside>
  )
}

// ── Spans page ────────────────────────────────────────────────────────────────

export default function Spans() {
  const [searchParams, setSearchParams] = useSearchParams()
  const initialDrilldown = drilldownFromURL(searchParams)
  const [view, setViewState] = useState<SpanView>(() => {
    const fromURL = searchParams.get('view')
    if (fromURL === 'all' || fromURL === 'grouped') return fromURL
    return localStorage.getItem('spaniel.spans.view') === 'grouped' ? 'grouped' : 'all'
  })
  useEffect(() => {
    const fromURL = searchParams.get('view')
    if (fromURL === 'all' || fromURL === 'grouped') setViewState(fromURL)
  }, [searchParams])
  const [sortBy, setSortBy] = useState<SpanSort>('time')
  const [sortMenuOpen, setSortMenuOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [svcSel, setSvcSel] = useState<string | null>(null)
  const [kindSel, setKindSel] = useState<string | null>(null)
  const [tagSel, setTagSel] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(() => searchParams.get('span'))
  const [page, setPage] = useState(1)
  const [drilldown, setDrilldown] = useState<SpanDrilldown | null>(() => initialDrilldown)
  const showingGroups = view === 'grouped' && drilldown === null

  const updateURL = (change: (params: URLSearchParams) => void) => {
    const params = new URLSearchParams(searchParams)
    change(params)
    setSearchParams(params, { replace: true })
  }

  const clearSelectedSpan = () => {
    setSelectedId(null)
    updateURL((params) => params.delete('span'))
  }

  const selectSpan = (spanID: string) => {
    setSelectedId(spanID)
    updateURL((params) => params.set('span', spanID))
  }

  const openGroup = (group: SpanDrilldown) => {
    setDrilldown(group)
    setPage(1)
    setSelectedId(null)
    updateURL((params) => {
      params.set('view', 'grouped')
      params.set('groupService', group.service_name)
      params.set('groupName', group.name)
      params.set('groupKind', String(group.kind))
      params.delete('span')
    })
  }

  useEffect(() => {
    const urlDrilldown = drilldownFromURL(searchParams)
    setDrilldown(urlDrilldown)
    setSelectedId(searchParams.get('span'))
  }, [searchParams])

  const setView = (next: SpanView) => {
    setViewState(next)
    localStorage.setItem('spaniel.spans.view', next)
    updateURL((params) => {
      params.set('view', next)
      params.delete('span')
      if (next === 'grouped') {
        params.delete('groupService')
        params.delete('groupName')
        params.delete('groupKind')
      }
    })
    setPage(1)
    setSelectedId(null)
    if (next === 'grouped') setDrilldown(null)
  }

  // sort is part of the server request, so it goes in the query key; live span
  // events refresh this via useLiveInvalidation() in App.tsx.
  const {
    data: spanResponse,
    isLoading: loading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.spans({ sort: sortBy, page, drilldown }),
    queryFn: () =>
      api.spans.list({
        sort: sortBy,
        page,
        limit: PAGE_SIZE,
        service: drilldown?.service_name,
        name: drilldown?.name,
        kind: drilldown?.kind,
      }),
    enabled: view === 'all' || drilldown !== null,
    // Keep the current page visible while a newly sorted page is loading.
    placeholderData: keepPreviousData,
  })
  const spans = useMemo(() => spanResponse?.data ?? [], [spanResponse])
  const arrivingSpanIDs = useNewItemIDs(spans, (span) => span.span_id)
  const spanTotal = spanResponse?.meta?.total ?? 0
  const { data: selectedSpan } = useQuery({
    queryKey: ['span', selectedId],
    queryFn: () => api.spans.get(selectedId!),
    enabled: selectedId !== null,
  })
  const {
    data: groupResponse,
    isLoading: groupsLoading,
    isError: groupsError,
    error: groupsErrorDetail,
    refetch: refetchGroups,
  } = useQuery({
    queryKey: qk.spans({ view: 'grouped', page }),
    queryFn: () => api.spans.groups({ page, limit: PAGE_SIZE }),
    enabled: showingGroups,
    // Pagination and switching back to an already-loaded groups page should
    // remain responsive while the aggregate refreshes.
    placeholderData: keepPreviousData,
  })
  const groups = groupResponse?.data ?? []
  const groupTotal = groupResponse?.meta?.total ?? 0

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
  const columns = useMemo<ColumnDef<SpanRow>[]>(
    () => [
      { id: 'service', accessorFn: (s) => s.service_name, filterFn: 'equals' },
      { id: 'kind', accessorFn: (s) => KIND_LABELS[s.kind] ?? 'unknown', filterFn: 'equals' },
      { id: 'tag', accessorFn: (s) => s.tag ?? '', filterFn: 'equals' },
    ],
    [],
  )

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

  const filtered = table.getRowModel().rows.map((r) => r.original)
  // A deep link may point to a span outside the currently loaded page. Fetch
  // it directly so reloading that link always restores the inspector.
  const selected = filtered.find((s) => s.span_id === selectedId) ?? selectedSpan?.data ?? null

  const activeFilters: { label: string; clear: () => void }[] = []
  if (svcSel) activeFilters.push({ label: `svc: ${svcSel}`, clear: () => setSvcSel(null) })
  if (kindSel) activeFilters.push({ label: `kind: ${kindSel}`, clear: () => setKindSel(null) })
  if (tagSel) activeFilters.push({ label: `tag: ${tagSel}`, clear: () => setTagSel(null) })

  return (
    <div className="flex h-full overflow-hidden">
      {/* Sidebar */}
      {view === 'all' && (
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
              >
                {svc}
              </SbItem>
            ))}
          </SbGroup>

          <SbGroup title="kind">
            {kindFacets.map(([k, n]) => (
              <SbItem
                key={k}
                active={kindSel === k}
                count={n}
                onClick={() => setKindSel(kindSel === k ? null : k)}
              >
                {k}
              </SbItem>
            ))}
          </SbGroup>

          <SbGroup title="callouts">
            <SbItem
              active={tagSel === 'n+1'}
              dot="var(--destructive, #c0392b)"
              count={calloutCounts['n+1']}
              onClick={() => setTagSel(tagSel === 'n+1' ? null : 'n+1')}
            >
              n+1
            </SbItem>
            <SbItem
              active={tagSel === 'slow'}
              dot="var(--warn, #d97706)"
              count={calloutCounts.slow}
              onClick={() => setTagSel(tagSel === 'slow' ? null : 'slow')}
            >
              slow (&gt;250ms)
            </SbItem>
            <SbItem
              active={tagSel === 'lint'}
              dot="var(--warn, #d97706)"
              count={calloutCounts.lint}
              onClick={() => setTagSel(tagSel === 'lint' ? null : 'lint')}
            >
              lint warnings
            </SbItem>
            <SbItem
              active={tagSel === 'error'}
              dot="var(--destructive, #c0392b)"
              count={calloutCounts.error}
              onClick={() => setTagSel(tagSel === 'error' ? null : 'error')}
            >
              errored
            </SbItem>
          </SbGroup>
        </aside>
      )}

      {/* Main */}
      <div className="flex-1 min-w-0 flex flex-col overflow-hidden bg-background">
        {/* Search + filters bar */}
        <div className="px-3.5 py-2.5 border-b border-border flex items-center gap-2.5 flex-wrap">
          <div
            className="inline-flex h-[30px] overflow-hidden rounded-lg border border-border bg-muted font-mono text-[11px]"
            data-testid="spans-view-toggle"
          >
            {(['all', 'grouped'] as const).map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setView(option)}
                className={`border-0 px-2.5 cursor-pointer ${view === option ? 'bg-[var(--accent)] text-white' : 'bg-transparent text-muted-foreground'}`}
              >
                {option === 'all' ? 'All' : 'Grouped'}
              </button>
            ))}
          </div>
          <div className="inline-flex items-center gap-[7px] bg-muted border border-border rounded-lg px-2.5 h-[30px] flex-1 min-w-[240px]">
            <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
              <circle cx="6" cy="6" r="4" stroke="var(--muted-foreground)" strokeWidth="1.4" />
              <line
                x1="9.2"
                y1="9.2"
                x2="12"
                y2="12"
                stroke="var(--muted-foreground)"
                strokeWidth="1.4"
                strokeLinecap="round"
              />
            </svg>
            <input
              data-testid="spans-search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={
                showingGroups
                  ? 'search the current page of operations…'
                  : 'search by name, attribute key or value, trace id…'
              }
              className="flex-1 border-none outline-none bg-transparent font-mono text-xs text-foreground"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="bg-transparent border-none cursor-pointer font-mono text-muted-foreground text-[11px] p-0"
              >
                clear
              </button>
            )}
          </div>

          {activeFilters.map((f) => (
            <FilterChip key={f.label} label={f.label} onClear={f.clear} />
          ))}

          {!showingGroups && (
            <div className="relative inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground px-2.5 h-[30px] rounded-md bg-muted border border-border">
              <span>sort</span>
              <button
                type="button"
                data-testid="spans-sort"
                aria-haspopup="menu"
                aria-expanded={sortMenuOpen}
                onClick={() => setSortMenuOpen((open) => !open)}
                className="inline-flex items-center gap-1 border-0 bg-transparent p-0 font-mono text-[11px] text-foreground cursor-pointer"
              >
                {SPAN_SORTS.find((option) => option.value === sortBy)?.label}
                <span aria-hidden="true" className="text-muted-foreground">
                  ▾
                </span>
              </button>
              {sortMenuOpen && (
                <div
                  role="menu"
                  aria-label="Sort spans"
                  className="absolute right-0 top-[34px] z-30 min-w-[126px] overflow-hidden rounded-md border border-border bg-background py-1 shadow-lg"
                >
                  {SPAN_SORTS.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      role="menuitemradio"
                      aria-checked={sortBy === option.value}
                      onClick={() => {
                        setSortBy(option.value)
                        setSortMenuOpen(false)
                        setPage(1)
                        clearSelectedSpan()
                      }}
                      className={`block w-full border-0 px-3 py-1.5 text-left font-mono text-[11px] cursor-pointer ${sortBy === option.value ? 'bg-muted text-foreground' : 'bg-background text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                    >
                      {option.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {drilldown && (
          <div className="flex items-center gap-2 px-3.5 py-2 border-b border-border bg-muted font-mono text-[11px]">
            <button
              type="button"
              onClick={() => {
                setDrilldown(null)
                setPage(1)
                updateURL((params) => {
                  params.delete('groupService')
                  params.delete('groupName')
                  params.delete('groupKind')
                })
              }}
              className="border-0 bg-transparent cursor-pointer text-[var(--accent)]"
            >
              ← all groups
            </button>
            <span className="text-muted-foreground">
              {drilldown.service_name} / {drilldown.name}
            </span>
          </div>
        )}

        {/* Column headers */}
        <div
          className="grid gap-2.5 px-3.5 py-[7px] border-b border-border font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] bg-muted"
          style={{
            gridTemplateColumns: showingGroups
              ? 'minmax(0,1.3fr) 120px 60px 65px 90px 60px 60px 60px 60px'
              : 'minmax(0,1.5fr) 130px 70px 70px 100px 130px 60px',
          }}
        >
          {showingGroups ? (
            <>
              <div>operation</div>
              <div>service</div>
              <div>kind</div>
              <div className="text-right">count</div>
              <div>latest</div>
              <div className="text-right">errors</div>
              <div className="text-right">p50</div>
              <div className="text-right">p95</div>
              <div className="text-right">max</div>
            </>
          ) : (
            <>
              <div>name</div>
              <div>service</div>
              <div>kind</div>
              <div className="text-right">dur</div>
              <div>started</div>
              <div>trace</div>
              <div className="text-right">tag</div>
            </>
          )}
        </div>

        {/* Rows */}
        <div className="flex-1 overflow-x-hidden overflow-y-auto">
          {(showingGroups ? groupsLoading : loading) ? (
            <div className="px-5 py-10 text-center font-mono text-[11px] text-muted-foreground">
              loading…
            </div>
          ) : (showingGroups ? groupsError : isError) ? (
            <ErrorState
              what="spans"
              error={showingGroups ? groupsErrorDetail : error}
              onRetry={() => (showingGroups ? refetchGroups() : refetch())}
            />
          ) : showingGroups && groups.length === 0 ? (
            <div className="px-5 py-10 text-center font-mono text-[11px] text-muted-foreground">
              no operations match — clear the search or try another page
            </div>
          ) : showingGroups ? (
            groups
              .filter((g) =>
                `${g.name} ${g.service_name}`.toLowerCase().includes(query.trim().toLowerCase()),
              )
              .map((g) => (
                <button
                  key={`${g.service_name}:${g.name}:${g.kind}`}
                  type="button"
                  data-testid={`span-group-${g.service_name}-${g.name}`}
                  onClick={() => openGroup(g)}
                  className="grid text-left cursor-pointer gap-2.5 px-3.5 py-2 items-center border-none w-full border-b border-border bg-transparent"
                  style={{
                    gridTemplateColumns: 'minmax(0,1.3fr) 120px 60px 65px 90px 60px 60px 60px 60px',
                  }}
                >
                  <div className="font-mono text-[11.5px] text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                    {g.name}
                    <span className="ml-2 text-[10px] text-muted-foreground">
                      {g.attribute_variants} attribute {g.attribute_variants === 1 ? 'set' : 'sets'}
                    </span>
                  </div>
                  <div>
                    <SvcChip name={g.service_name} />
                  </div>
                  <div className="font-mono text-[10px] text-muted-foreground">
                    {KIND_LABELS[g.kind] ?? 'unknown'}
                  </div>
                  <div className="text-right font-mono text-[11.5px]">{g.count}</div>
                  <div className="font-mono text-[10px] text-muted-foreground">
                    {fmtClock(g.latest_start_ns)}
                  </div>
                  <div
                    className="text-right font-mono text-[11.5px]"
                    style={{ color: g.error_count ? 'var(--destructive)' : undefined }}
                  >
                    {g.error_count || '—'}
                  </div>
                  <div className="text-right font-mono text-[11.5px]">
                    {fmtDuration(g.p50_duration_ns)}
                  </div>
                  <div className="text-right font-mono text-[11.5px]">
                    {fmtDuration(g.p95_duration_ns)}
                  </div>
                  <div className="text-right font-mono text-[11.5px]">
                    {fmtDuration(g.max_duration_ns)}
                  </div>
                </button>
              ))
          ) : filtered.length === 0 ? (
            spans.length === 0 ? (
              <EmptyState
                title="No spans recorded"
                hint="Send any OTLP span — they'll appear here."
                glyph={
                  <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                    <rect
                      x="4"
                      y="13"
                      width="18"
                      height="4"
                      rx="2"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      opacity="0.6"
                    />
                    <rect
                      x="10"
                      y="21"
                      width="14"
                      height="4"
                      rx="2"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      opacity="0.4"
                    />
                    <rect
                      x="7"
                      y="5"
                      width="10"
                      height="4"
                      rx="2"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      opacity="0.5"
                    />
                  </svg>
                }
                cta={{
                  label: 'OTLP docs →',
                  href: 'https://opentelemetry.io/docs/specs/otel/protocol/',
                }}
              />
            ) : (
              <div className="px-5 py-10 text-center font-mono text-[11px] text-muted-foreground">
                no spans match — clear a filter or try a different query
              </div>
            )
          ) : (
            filtered.map((s) => {
              const isSel = s.span_id === selectedId
              const isSlow = s.duration_ns > SLOW_NS
              const kindLabel = KIND_LABELS[s.kind] ?? 'unknown'
              return (
                <TelemetryArrival key={s.span_id} arriving={arrivingSpanIDs.has(s.span_id)}>
                  <button
                    type="button"
                    data-testid={`span-row-${s.span_id}`}
                    onClick={() => (isSel ? clearSelectedSpan() : selectSpan(s.span_id))}
                    className={`grid text-left cursor-pointer gap-2.5 px-3.5 py-2 items-center border-none w-full border-b border-border outline-none border-l-2 transition-colors ${isSel ? 'border-l-[var(--accent,#6366f1)]' : 'border-l-transparent bg-transparent'}`}
                    style={{
                      gridTemplateColumns: 'minmax(0,1.5fr) 130px 70px 70px 100px 130px 60px',
                      background: isSel
                        ? 'color-mix(in oklch, var(--accent, #6366f1) 14%, var(--background))'
                        : undefined,
                    }}
                  >
                    <div
                      title={httpDisplayName(s)}
                      className="font-mono text-[11.5px] text-foreground overflow-hidden text-ellipsis whitespace-nowrap"
                    >
                      {httpDisplayName(s)}
                    </div>
                    <div>
                      <SvcChip name={s.service_name} />
                    </div>
                    <div className="font-mono text-[10px] text-muted-foreground uppercase tracking-[0.08em]">
                      {kindLabel}
                    </div>
                    <div
                      className="text-right font-mono text-[11.5px] font-semibold"
                      style={{
                        color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)',
                      }}
                    >
                      {fmtDuration(s.duration_ns)}
                    </div>
                    <div className="font-mono text-[10px] text-muted-foreground whitespace-nowrap">
                      {fmtClock(s.start_ns)}
                    </div>
                    <div
                      title={s.trace_id}
                      className="font-mono text-[10px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap"
                    >
                      {s.trace_id}
                    </div>
                    <div className="text-right">
                      <TagBadge tag={s.tag} />
                    </div>
                  </button>
                </TelemetryArrival>
              )
            })
          )}
        </div>

        {/* Status footer */}
        <div className="px-3.5 py-2 border-t border-border bg-muted font-mono text-[10.5px] text-muted-foreground flex items-center gap-3.5">
          <span className="flex-1" />
          <PaginationControls
            page={page}
            pageSize={PAGE_SIZE}
            total={showingGroups ? groupTotal : spanTotal}
            itemLabel={showingGroups ? 'operations' : 'spans'}
            onPageChange={setPage}
          />
          <code className="px-2 py-[3px] rounded-[5px] border border-border bg-background text-muted-foreground font-mono text-[10px]">
            spaniel spans --tail
          </code>
        </div>
      </div>

      {/* Inspector */}
      <AnimatePresence mode="wait" initial={false}>
        {selected && (
          <motion.div
            key={selected.span_id}
            className="shrink-0"
            initial={{ opacity: 0, x: 8 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 8 }}
            transition={{ duration: 0.16, ease: [0.2, 0, 0, 1] }}
          >
            <SpanInspector span={selected} onClose={clearSelectedSpan} />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
