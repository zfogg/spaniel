import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { qk } from '@/lib/query'
import { api, ServiceCoverage, CoverageRoute } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import EmptyState from '@/components/EmptyState'
import ErrorState from '@/components/ErrorState'

type Filter = 'all' | 'dark' | 'observed'

// ── helpers ──────────────────────────────────────────────────────────────────

function coverageColor(pct: number): string {
  if (pct >= 90) return 'var(--ok, #3e6a3e)'
  if (pct >= 70) return '#a5a64d'
  if (pct >= 40) return 'var(--warn, #b58400)'
  return 'var(--danger, #b04040)'
}

function methodTone(method: string): { fg: string; bg: string } {
  switch (method) {
    case 'GET':    return { fg: '#356a99', bg: '#cfdde9' }
    case 'POST':   return { fg: '#3e6a3e', bg: '#cfe2cf' }
    case 'PUT':
    case 'PATCH':  return { fg: '#7a6018', bg: '#ecdba6' }
    case 'DELETE': return { fg: '#7a3a23', bg: '#ecc8b3' }
    case 'RPC':    return { fg: '#5a3f6e', bg: '#dccdec' }
    case '':       return { fg: 'var(--muted-foreground)', bg: 'var(--muted)' }
    default:       return { fg: 'var(--muted-foreground)', bg: 'var(--muted)' }
  }
}

function fmtNs(ns: number): string {
  if (ns < 1_000) return `${ns}ns`
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

// ── atoms ────────────────────────────────────────────────────────────────────

function CoverageBar({ pct, big = false }: { pct: number; big?: boolean }) {
  return (
    <div
      className={`${big ? 'h-3' : 'h-2'} rounded-lg bg-muted border border-border overflow-hidden relative w-full`}
    >
      <div
        className="absolute left-0 top-0 bottom-0 transition-[width] duration-200"
        style={{ width: `${pct}%`, background: coverageColor(pct) }}
      />
    </div>
  )
}

function HeatCell({
  route, selected, onClick,
}: {
  route: CoverageRoute; selected: boolean; onClick: () => void
}) {
  const dark = route.hits === 0
  const intensity = dark ? 0 : Math.min(1, 0.18 + Math.log10(route.hits + 1) / 3)
  const m = methodTone(route.method)
  return (
    <button
      type="button"
      onClick={onClick}
      title={`${route.method} ${route.path} · ${route.hits} traces`}
      className="relative cursor-pointer text-left pt-[7px] pr-[9px] pb-[7px] pl-[11px] rounded-md font-mono text-[11px] outline-hidden flex items-center gap-[7px] min-w-0"
      style={{
        background: dark
          ? `repeating-linear-gradient(45deg,
              color-mix(in oklch, var(--danger, #b04040) 18%, var(--background)) 0 5px,
              color-mix(in oklch, var(--danger, #b04040) 8%, var(--background)) 5px 10px)`
          : `color-mix(in oklch, ${m.fg} ${intensity * 100}%, var(--background))`,
        border: '1px solid ' + (dark ? 'var(--danger, #b04040)' : selected ? m.fg : 'var(--border)'),
        boxShadow: selected ? `inset 0 0 0 1px ${dark ? 'var(--danger, #b04040)' : m.fg}` : 'none',
      }}
    >
      <span
        className="font-mono text-[9px] font-bold rounded-[3px] px-1 tracking-[0.05em] whitespace-nowrap shrink-0"
        style={{
          color: m.fg,
          background: dark ? 'var(--background)' : 'transparent',
          border: `1px solid ${m.fg}`,
        }}
      >{route.method || '·'}</span>
      <span
        className={`flex-1 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap ${dark ? 'font-semibold text-danger' : 'font-medium text-foreground'}`}
      >{route.path}</span>
      <span
        className={`font-mono text-[9.5px] ${dark ? 'font-bold text-danger' : 'font-medium text-muted-foreground'}`}
      >{dark ? 'DARK' : route.hits.toLocaleString()}</span>
    </button>
  )
}

function BigStat({ label, value, sub, tone, mono }: {
  label: string; value: string | number; sub?: string; tone?: 'danger'; mono?: boolean
}) {
  return (
    <div className="py-[18px] px-5 border-r border-border">
      <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">{label}</div>
      <div
        className={`${mono ? 'font-mono text-lg' : 'font-serif text-[32px]'} font-semibold mt-1 leading-[1.05] ${tone === 'danger' ? 'text-danger' : 'text-foreground'}`}
      >{value}</div>
      {sub && (
        <div className="font-mono text-[10px] text-muted-foreground mt-1.5">{sub}</div>
      )}
    </div>
  )
}

// ── service section ─────────────────────────────────────────────────────────

function ServiceSection({
  svc, filter, selected, onSelect, expanded, onToggle,
}: {
  svc: ServiceCoverage
  filter: Filter
  selected: { svc: string; m: string; p: string } | null
  onSelect: (svc: string, r: CoverageRoute) => void
  expanded: boolean
  onToggle: () => void
}) {
  const allRoutes = [...svc.observed_routes, ...svc.dark_routes]
  const filtered = allRoutes.filter(r => {
    if (filter === 'dark') return r.hits === 0
    if (filter === 'observed') return r.hits > 0
    return true
  })
  const c = svcColor(svc.name).fg

  return (
    <section className="bg-background border border-border rounded-[10px] mb-3.5 overflow-hidden">
      <button
        type="button"
        onClick={onToggle}
        className="w-full py-3 px-4 cursor-pointer grid gap-3.5 items-center bg-transparent border-none outline-hidden text-left"
        style={{ gridTemplateColumns: 'auto minmax(140px, 1fr) 220px 90px 80px 64px' }}
      >
        <span
          className="inline-flex w-3 h-3 text-muted-foreground transition-transform duration-150"
          style={{ transform: expanded ? 'rotate(90deg)' : 'rotate(0)' }}
          aria-hidden
        >▶</span>

        <div className="flex items-center gap-2.5 min-w-0">
          <span
            className="font-mono text-[11px] font-semibold py-0.5 px-[7px] rounded whitespace-nowrap"
            style={{ color: c, background: svcColor(svc.name).bg }}
          >{svc.name}</span>
          {svc.source === 'openapi' ? (
            <span className="py-0.5 px-[7px] rounded-[5px] bg-muted text-foreground border border-border font-mono text-[9px] font-semibold">★ {svc.spec}</span>
          ) : (
            <span className="py-0.5 px-[7px] rounded-[5px] bg-muted text-muted-foreground border border-border font-mono text-[9px] font-semibold">observed only · no spec</span>
          )}
        </div>

        <CoverageBar pct={svc.coverage_pct} />

        <div className="flex flex-col gap-0.5">
          <span
            className="font-serif text-lg font-semibold leading-none"
            style={{ color: coverageColor(svc.coverage_pct) }}
          >{svc.coverage_pct.toFixed(1)}<span className="text-[11px] ml-px">%</span></span>
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">coverage</span>
        </div>
        <div className="flex flex-col gap-0.5">
          <span
            className={`font-serif text-lg font-semibold leading-none ${svc.dark_routes.length > 0 ? 'text-danger' : 'text-muted-foreground'}`}
          >{svc.dark_routes.length}</span>
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">dark</span>
        </div>
        <div className="flex flex-col gap-0.5">
          <span className="font-serif text-lg font-semibold leading-none text-foreground">{svc.total_routes}</span>
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">total</span>
        </div>
      </button>

      {expanded && (
        <div
          data-testid={`routes-${svc.name}`}
          className="pt-1 px-3.5 pb-3.5 grid gap-2 border-t border-border bg-muted"
          style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))' }}
        >
          {filtered.length === 0 ? (
            <div
              className="py-3.5 px-1.5 font-mono text-[11px] text-muted-foreground text-center"
              style={{ gridColumn: '1 / -1' }}
            >no routes match this filter</div>
          ) : filtered.map(r => (
            <HeatCell
              key={`${r.method}-${r.path}`}
              route={r}
              selected={!!selected && selected.svc === svc.name && selected.m === r.method && selected.p === r.path}
              onClick={() => onSelect(svc.name, r)}
            />
          ))}
        </div>
      )}
    </section>
  )
}

// ── route inspect rail ──────────────────────────────────────────────────────

function RouteInspect({ svc, route }: { svc: string; route: CoverageRoute }) {
  const dark = route.hits === 0
  const m = methodTone(route.method)
  return (
    <aside className="w-[340px] border-l border-border bg-background flex flex-col overflow-auto">
      <div className="py-3.5 px-4 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <span
            className="font-mono text-[10px] font-semibold py-px px-[7px] rounded"
            style={{ color: svcColor(svc).fg, background: svcColor(svc).bg }}
          >{svc}</span>
          <span
            className={`py-0.5 px-[7px] rounded text-white font-mono text-[9px] font-bold uppercase tracking-[0.08em] ${dark ? 'bg-danger' : 'bg-[#3e6a3e]'}`}
          >{dark ? 'dark' : 'observed'}</span>
        </div>
        <div className="flex items-baseline gap-2">
          <span
            className="font-mono text-[11px] font-bold py-px px-1.5 rounded tracking-[0.05em]"
            style={{ color: m.fg, border: `1px solid ${m.fg}` }}
          >{route.method || '·'}</span>
          <span className="font-mono text-sm text-foreground font-semibold break-all">{route.path}</span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-0 border-b border-border">
        <div className="py-3.5 px-4 border-r border-border">
          <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">traces seen</div>
          <div
            className={`font-serif text-2xl font-semibold leading-[1.1] mt-1 ${dark ? 'text-danger' : 'text-foreground'}`}
          >{route.hits.toLocaleString()}</div>
        </div>
        <div className="py-3.5 px-4">
          <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">p95</div>
          <div className="font-serif text-2xl font-semibold leading-[1.1] mt-1 text-foreground">{route.p95_ns ? fmtNs(route.p95_ns) : '—'}</div>
        </div>
      </div>

      {dark && (
        <div className="py-3.5 px-4">
          <div
            className="py-2.5 px-3 rounded-lg"
            style={{
              background: 'color-mix(in oklch, var(--danger, #b04040) 14%, var(--background))',
              border: '1px solid color-mix(in oklch, var(--danger, #b04040) 35%, transparent)',
            }}
          >
            <div className="font-mono text-[10px] font-bold text-danger tracking-[0.08em] mb-[5px]">● no traces — instrument this route</div>
            <div className="font-sans text-xs text-foreground leading-[1.5]">
              Spaniel hasn't seen any span with{' '}
              <code className="font-mono text-[11px] bg-muted py-px px-1 rounded-[3px]">http.route = "{route.path}"</code>.
            </div>
          </div>
        </div>
      )}
    </aside>
  )
}

// ── page ────────────────────────────────────────────────────────────────────

export default function Coverage() {
  const [filter, setFilter] = useState<Filter>('all')
  const [selected, setSelected] = useState<{ svc: string; route: CoverageRoute } | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  const { data: report = null, isLoading: loading, isError, error, refetch } = useQuery({
    queryKey: qk.coverage(),
    queryFn: () => api.coverage.get().then(r => r.data),
  })

  const totalDark = useMemo(() => report?.overall.dark_count ?? 0, [report])

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">Loading coverage…</div>
    )
  }

  if (isError) {
    return <ErrorState what="coverage" error={error} onRetry={() => refetch()} />
  }

  if (!report || report.services.length === 0) {
    return (
      <EmptyState
        title="No coverage data"
        hint={<>Send traces with <code>http.route</code> attributes (or import an OpenAPI spec with <code>--routes-file</code>) to populate this view.</>}
        glyph={
          <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
            <circle cx="16" cy="16" r="11" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
            <path d="M16 5 A11 11 0 0 1 27 16" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" opacity="0.8" />
            <circle cx="16" cy="16" r="2.5" fill="currentColor" opacity="0.6" />
          </svg>
        }
      />
    )
  }

  const sel = selected
  const overall = report.overall

  return (
    <div className="flex-1 flex min-h-0 overflow-hidden">
      {/* left rail */}
      <div className="w-[220px] border-r border-border bg-muted py-3.5 px-3 flex flex-col gap-4 shrink-0">
        <div>
          <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-2">filter</div>
          {([
            ['all', 'all routes', overall.total_routes],
            ['dark', 'dark only', totalDark],
            ['observed', 'observed only', overall.observed_operations],
          ] as const).map(([key, label, count]) => {
            const on = filter === key
            return (
              <button
                key={key}
                type="button"
                onClick={() => setFilter(key)}
                className={`w-full flex items-center gap-1.5 py-1.5 px-2 rounded-md cursor-pointer text-foreground font-mono text-[11px] outline-hidden text-left ${on ? 'bg-background border border-border' : 'bg-transparent border border-transparent'}`}
              >
                <span
                  className="w-1.5 h-1.5 rounded-full"
                  style={{
                    background: key === 'dark' ? 'var(--danger, #b04040)'
                      : key === 'observed' ? '#3e6a3e'
                      : 'var(--foreground)',
                  }}
                />
                <span className="flex-1">{label}</span>
                <span className="text-muted-foreground">{count}</span>
              </button>
            )
          })}
        </div>
      </div>

      {/* main */}
      <div className="flex-1 overflow-x-hidden overflow-y-auto">
        <div className="pt-[22px] px-6 pb-1">
          <div className="font-mono text-[10px] text-muted-foreground uppercase tracking-[0.18em] mb-1.5">instrumentation coverage</div>
          <h1 className="m-0 font-serif text-[28px] font-semibold tracking-[-0.02em] text-foreground">Coverage</h1>
          <div className="font-sans text-[13px] text-muted-foreground mt-1 max-w-[720px]">
            Which routes have we ever seen a trace for? Spaniel cross-references every{' '}
            <code className="font-mono text-xs bg-muted py-px px-[5px] rounded">http.route</code> against any declared specs to find the dark ones.
          </div>
        </div>

        <div className="pt-[18px] px-6 pb-1">
          {/* overall card */}
          <div
            className="grid gap-0 mb-[18px] bg-background border border-border rounded-[10px] overflow-hidden"
            style={{ gridTemplateColumns: '1.4fr 1fr 1fr 1fr' }}
          >
            <div className="py-[18px] px-5 border-r border-border">
              <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">overall</div>
              <div className="flex items-baseline gap-1.5 mt-1">
                <span
                  className="font-serif text-[40px] font-semibold leading-none"
                  style={{ color: coverageColor(overall.coverage_pct) }}
                >{overall.coverage_pct.toFixed(1)}</span>
                <span className="font-serif text-xl text-muted-foreground">%</span>
                <span className="font-mono text-[11px] text-muted-foreground ml-2">
                  {overall.observed_operations} of {overall.total_routes} routes
                </span>
              </div>
              <div className="mt-3.5">
                <CoverageBar pct={overall.coverage_pct} big />
              </div>
            </div>
            <BigStat label="dark" value={overall.dark_count} tone="danger"
              sub={`across ${report.services.filter(s => s.dark_routes.length > 0).length} services`} />
            <BigStat label="services" value={report.services.length}
              sub={`${report.services.filter(s => s.source === 'openapi').length} have specs`} />
            <BigStat label="best" mono value={
              report.services.reduce((b, s) => s.coverage_pct > b.coverage_pct ? s : b, report.services[0]).name
            } sub="highest coverage" />
          </div>

          {report.services.map(svc => (
            <ServiceSection
              key={svc.name}
              svc={svc}
              filter={filter}
              expanded={expanded[svc.name] ?? true}
              onToggle={() => setExpanded(p => ({ ...p, [svc.name]: !(p[svc.name] ?? true) }))}
              selected={sel ? { svc: sel.svc, m: sel.route.method, p: sel.route.path } : null}
              onSelect={(svcName, r) => setSelected({ svc: svcName, route: r })}
            />
          ))}

          <div className="h-[30px]" />
        </div>
      </div>

      {sel && <RouteInspect svc={sel.svc} route={sel.route} />}
    </div>
  )
}
