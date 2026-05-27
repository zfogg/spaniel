import { useEffect, useMemo, useState } from 'react'
import { api, CoverageReport, ServiceCoverage, CoverageRoute } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'

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
    <div style={{
      height: big ? 12 : 8, borderRadius: 8,
      background: 'var(--muted)', border: '1px solid var(--border)',
      overflow: 'hidden', position: 'relative', width: '100%',
    }}>
      <div style={{
        position: 'absolute', left: 0, top: 0, bottom: 0,
        width: `${pct}%`, background: coverageColor(pct),
        transition: 'width .2s',
      }} />
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
      style={{
        position: 'relative', cursor: 'pointer', textAlign: 'left',
        padding: '7px 9px 7px 11px', borderRadius: 6,
        background: dark
          ? `repeating-linear-gradient(45deg,
              color-mix(in oklch, var(--danger, #b04040) 18%, var(--background)) 0 5px,
              color-mix(in oklch, var(--danger, #b04040) 8%, var(--background)) 5px 10px)`
          : `color-mix(in oklch, ${m.fg} ${intensity * 100}%, var(--background))`,
        border: '1px solid ' + (dark ? 'var(--danger, #b04040)' : selected ? m.fg : 'var(--border)'),
        boxShadow: selected ? `inset 0 0 0 1px ${dark ? 'var(--danger, #b04040)' : m.fg}` : 'none',
        fontFamily: 'var(--font-mono)', fontSize: 11,
        outline: 'none',
        display: 'flex', alignItems: 'center', gap: 7, minWidth: 0,
      }}
    >
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 700,
        color: m.fg, background: dark ? 'var(--background)' : 'transparent',
        border: `1px solid ${m.fg}`, borderRadius: 3, padding: '0 4px',
        letterSpacing: '0.05em', whiteSpace: 'nowrap', flex: '0 0 auto',
      }}>{route.method || '·'}</span>
      <span style={{
        flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        fontWeight: dark ? 600 : 500,
        color: dark ? 'var(--danger, #b04040)' : 'var(--foreground)',
      }}>{route.path}</span>
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 9.5,
        fontWeight: dark ? 700 : 500,
        color: dark ? 'var(--danger, #b04040)' : 'var(--muted-foreground)',
      }}>{dark ? 'DARK' : route.hits.toLocaleString()}</span>
    </button>
  )
}

function BigStat({ label, value, sub, tone, mono }: {
  label: string; value: string | number; sub?: string; tone?: 'danger'; mono?: boolean
}) {
  return (
    <div style={{ padding: '18px 20px', borderRight: '1px solid var(--border)' }}>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
        textTransform: 'uppercase', letterSpacing: '0.14em',
      }}>{label}</div>
      <div style={{
        fontFamily: mono ? 'var(--font-mono)' : 'var(--font-serif, serif)',
        fontSize: mono ? 18 : 32, fontWeight: 600,
        marginTop: 4, lineHeight: 1.05,
        color: tone === 'danger' ? 'var(--danger, #b04040)' : 'var(--foreground)',
      }}>{value}</div>
      {sub && (
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)', marginTop: 6 }}>{sub}</div>
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
    <section style={{
      background: 'var(--background)', border: '1px solid var(--border)',
      borderRadius: 10, marginBottom: 14, overflow: 'hidden',
    }}>
      <button
        type="button"
        onClick={onToggle}
        style={{
          width: '100%', padding: '12px 16px', cursor: 'pointer',
          display: 'grid',
          gridTemplateColumns: 'auto minmax(140px, 1fr) 220px 90px 80px 64px',
          gap: 14, alignItems: 'center',
          background: 'transparent', border: 'none', outline: 'none', textAlign: 'left',
        }}
      >
        <span style={{
          display: 'inline-flex', width: 12, height: 12,
          color: 'var(--muted-foreground)',
          transform: expanded ? 'rotate(90deg)' : 'rotate(0)',
          transition: 'transform .15s',
        }} aria-hidden>▶</span>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 600,
            color: c, background: svcColor(svc.name).bg,
            padding: '2px 7px', borderRadius: 4, whiteSpace: 'nowrap',
          }}>{svc.name}</span>
          {svc.source === 'openapi' ? (
            <span style={{
              padding: '2px 7px', borderRadius: 5,
              background: 'var(--muted)', color: 'var(--foreground)',
              border: '1px solid var(--border)',
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
            }}>★ {svc.spec}</span>
          ) : (
            <span style={{
              padding: '2px 7px', borderRadius: 5,
              background: 'var(--muted)', color: 'var(--muted-foreground)',
              border: '1px solid var(--border)',
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 600,
            }}>observed only · no spec</span>
          )}
        </div>

        <CoverageBar pct={svc.coverage_pct} />

        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{
            fontFamily: 'var(--font-serif, serif)', fontSize: 18, fontWeight: 600,
            lineHeight: 1, color: coverageColor(svc.coverage_pct),
          }}>{svc.coverage_pct.toFixed(1)}<span style={{ fontSize: 11, marginLeft: 1 }}>%</span></span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>coverage</span>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{
            fontFamily: 'var(--font-serif, serif)', fontSize: 18, fontWeight: 600, lineHeight: 1,
            color: svc.dark_routes.length > 0 ? 'var(--danger, #b04040)' : 'var(--muted-foreground)',
          }}>{svc.dark_routes.length}</span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>dark</span>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{
            fontFamily: 'var(--font-serif, serif)', fontSize: 18, fontWeight: 600, lineHeight: 1,
            color: 'var(--foreground)',
          }}>{svc.total_routes}</span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>total</span>
        </div>
      </button>

      {expanded && (
        <div
          data-testid={`routes-${svc.name}`}
          style={{
            padding: '4px 14px 14px',
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
            gap: 8,
            borderTop: '1px solid var(--border)',
            background: 'var(--muted)',
          }}
        >
          {filtered.length === 0 ? (
            <div style={{
              gridColumn: '1 / -1', padding: '14px 6px',
              fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)',
              textAlign: 'center',
            }}>no routes match this filter</div>
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
    <aside style={{
      width: 340, borderLeft: '1px solid var(--border)',
      background: 'var(--background)',
      display: 'flex', flexDirection: 'column', overflow: 'auto',
    }}>
      <div style={{ padding: '14px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600,
            color: svcColor(svc).fg, background: svcColor(svc).bg,
            padding: '1px 7px', borderRadius: 4,
          }}>{svc}</span>
          <span style={{
            padding: '2px 7px', borderRadius: 4,
            background: dark ? 'var(--danger, #b04040)' : '#3e6a3e',
            color: 'white',
            fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 700,
            textTransform: 'uppercase', letterSpacing: '0.08em',
          }}>{dark ? 'dark' : 'observed'}</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 700,
            color: m.fg, border: `1px solid ${m.fg}`,
            padding: '1px 6px', borderRadius: 4, letterSpacing: '0.05em',
          }}>{route.method || '·'}</span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 14, color: 'var(--foreground)',
            fontWeight: 600, wordBreak: 'break-all',
          }}>{route.path}</span>
        </div>
      </div>

      <div style={{
        display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 0,
        borderBottom: '1px solid var(--border)',
      }}>
        <div style={{ padding: '14px 16px', borderRight: '1px solid var(--border)' }}>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>traces seen</div>
          <div style={{
            fontFamily: 'var(--font-serif, serif)', fontSize: 24, fontWeight: 600,
            lineHeight: 1.1, marginTop: 4,
            color: dark ? 'var(--danger, #b04040)' : 'var(--foreground)',
          }}>{route.hits.toLocaleString()}</div>
        </div>
        <div style={{ padding: '14px 16px' }}>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>p95</div>
          <div style={{
            fontFamily: 'var(--font-serif, serif)', fontSize: 24, fontWeight: 600,
            lineHeight: 1.1, marginTop: 4, color: 'var(--foreground)',
          }}>{route.p95_ns ? fmtNs(route.p95_ns) : '—'}</div>
        </div>
      </div>

      {dark && (
        <div style={{ padding: '14px 16px' }}>
          <div style={{
            padding: '10px 12px', borderRadius: 8,
            background: 'color-mix(in oklch, var(--danger, #b04040) 14%, var(--background))',
            border: '1px solid color-mix(in oklch, var(--danger, #b04040) 35%, transparent)',
          }}>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 700,
              color: 'var(--danger, #b04040)', letterSpacing: '0.08em', marginBottom: 5,
            }}>● no traces — instrument this route</div>
            <div style={{
              fontFamily: 'var(--font-sans)', fontSize: 12, color: 'var(--foreground)',
              lineHeight: 1.5,
            }}>
              Spaniel hasn't seen any span with{' '}
              <code style={{
                fontFamily: 'var(--font-mono)', fontSize: 11,
                background: 'var(--muted)', padding: '1px 4px', borderRadius: 3,
              }}>http.route = "{route.path}"</code>.
            </div>
          </div>
        </div>
      )}
    </aside>
  )
}

// ── page ────────────────────────────────────────────────────────────────────

export default function Coverage() {
  const [report, setReport] = useState<CoverageReport | null>(null)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<Filter>('all')
  const [selected, setSelected] = useState<{ svc: string; route: CoverageRoute } | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  useEffect(() => {
    let cancel = false
    api.coverage.get()
      .then(r => { if (!cancel) { setReport(r.data); setLoading(false) } })
      .catch(() => { if (!cancel) setLoading(false) })
    return () => { cancel = true }
  }, [])

  const totalDark = useMemo(() => report?.overall.dark_count ?? 0, [report])

  if (loading) {
    return (
      <div style={{
        flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
        color: 'var(--muted-foreground)', fontFamily: 'var(--font-mono)', fontSize: 13,
      }}>Loading coverage…</div>
    )
  }

  if (!report || report.services.length === 0) {
    return (
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 40 }}>
        <div style={{ textAlign: 'center', maxWidth: 460 }}>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)', marginBottom: 8,
          }}>No coverage data</div>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)', lineHeight: 1.5,
          }}>
            Send traces with <code>http.route</code> attributes (or import an OpenAPI spec with{' '}
            <code>--routes-file</code>) to populate this view.
          </div>
        </div>
      </div>
    )
  }

  const sel = selected
  const overall = report.overall

  return (
    <div style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
      {/* left rail */}
      <div style={{
        width: 220, borderRight: '1px solid var(--border)',
        background: 'var(--muted)', padding: '14px 12px',
        display: 'flex', flexDirection: 'column', gap: 16, flexShrink: 0,
      }}>
        <div>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em', marginBottom: 8,
          }}>filter</div>
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
                style={{
                  width: '100%', display: 'flex', alignItems: 'center', gap: 6,
                  padding: '6px 8px', borderRadius: 6, cursor: 'pointer',
                  background: on ? 'var(--background)' : 'transparent',
                  border: '1px solid ' + (on ? 'var(--border)' : 'transparent'),
                  color: 'var(--foreground)', fontFamily: 'var(--font-mono)', fontSize: 11,
                  outline: 'none', textAlign: 'left',
                }}
              >
                <span style={{
                  width: 6, height: 6, borderRadius: 6,
                  background: key === 'dark' ? 'var(--danger, #b04040)'
                    : key === 'observed' ? '#3e6a3e'
                    : 'var(--foreground)',
                }} />
                <span style={{ flex: 1 }}>{label}</span>
                <span style={{ color: 'var(--muted-foreground)' }}>{count}</span>
              </button>
            )
          })}
        </div>
      </div>

      {/* main */}
      <div style={{ flex: 1, overflow: 'hidden auto' }}>
        <div style={{ padding: '22px 24px 4px' }}>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.18em', marginBottom: 6,
          }}>instrumentation coverage</div>
          <h1 style={{
            margin: 0, fontFamily: 'var(--font-serif, serif)', fontSize: 28, fontWeight: 600,
            letterSpacing: '-0.02em', color: 'var(--foreground)',
          }}>Coverage</h1>
          <div style={{
            fontFamily: 'var(--font-sans)', fontSize: 13, color: 'var(--muted-foreground)',
            marginTop: 4, maxWidth: 720,
          }}>
            Which routes have we ever seen a trace for? Spaniel cross-references every{' '}
            <code style={{
              fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--muted)',
              padding: '1px 5px', borderRadius: 4,
            }}>http.route</code> against any declared specs to find the dark ones.
          </div>
        </div>

        <div style={{ padding: '18px 24px 4px' }}>
          {/* overall card */}
          <div style={{
            display: 'grid', gridTemplateColumns: '1.4fr 1fr 1fr 1fr',
            gap: 0, marginBottom: 18,
            background: 'var(--background)', border: '1px solid var(--border)',
            borderRadius: 10, overflow: 'hidden',
          }}>
            <div style={{ padding: '18px 20px', borderRight: '1px solid var(--border)' }}>
              <div style={{
                fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
                textTransform: 'uppercase', letterSpacing: '0.14em',
              }}>overall</div>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginTop: 4 }}>
                <span style={{
                  fontFamily: 'var(--font-serif, serif)', fontSize: 40, fontWeight: 600,
                  color: coverageColor(overall.coverage_pct), lineHeight: 1,
                }}>{overall.coverage_pct.toFixed(1)}</span>
                <span style={{ fontFamily: 'var(--font-serif, serif)', fontSize: 20, color: 'var(--muted-foreground)' }}>%</span>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)', marginLeft: 8 }}>
                  {overall.observed_operations} of {overall.total_routes} routes
                </span>
              </div>
              <div style={{ marginTop: 14 }}>
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

          <div style={{ height: 30 }} />
        </div>
      </div>

      {sel && <RouteInspect svc={sel.svc} route={sel.route} />}
    </div>
  )
}
