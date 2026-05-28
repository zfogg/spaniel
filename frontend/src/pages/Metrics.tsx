import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, MetricCatalogEntry, MetricSeries, TraceOverlay } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import { bucketPoints, type BucketedSeries, type MetricType } from '@/lib/metrics-bucket'
import { fmtVal, statsFor, type Stat } from '@/lib/metrics-format'
import { xPosForTrace, valueAtBin } from '@/lib/metrics-correlate'
import EmptyState from '@/components/EmptyState'

// ── icons ────────────────────────────────────────────────────────────────────

function MtIcon({ type }: { type: MetricType }) {
  if (type === 'gauge') {
    return (
      <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
        <path d="M2 10a5 5 0 0 1 10 0" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" fill="none" />
        <line x1="7" y1="10" x2="10" y2="6" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        <circle cx="7" cy="10" r="1" fill="currentColor" />
      </svg>
    )
  }
  if (type === 'counter') {
    return (
      <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
        <path d="M2 11 L5 7 L8 9 L12 3" stroke="currentColor" strokeWidth="1.4" fill="none" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M9 3h3v3" stroke="currentColor" strokeWidth="1.4" fill="none" strokeLinecap="round" />
      </svg>
    )
  }
  return (
    <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
      <rect x="2"  y="8" width="2" height="4" fill="currentColor" opacity="0.45" />
      <rect x="5"  y="5" width="2" height="7" fill="currentColor" opacity="0.7" />
      <rect x="8"  y="3" width="2" height="9" fill="currentColor" opacity="0.9" />
      <rect x="11" y="6" width="2" height="6" fill="currentColor" opacity="0.6" />
    </svg>
  )
}

function MetricKindTag({ type }: { type: MetricType }) {
  const tone =
    type === 'gauge' ? { fg: '#356a99', bd: '#7aa3c5', bg: '#dfe7ef' } :
    type === 'counter' ? { fg: '#3e6a3e', bd: '#88b29a', bg: '#dee9de' } :
    { fg: '#7a3a23', bd: '#c89a86', bg: '#ecd9cf' }
  return (
    <span
      className="inline-flex items-center gap-1 px-[7px] py-px rounded-[5px] font-mono text-[9px] font-bold tracking-[0.08em] uppercase border"
      style={{ color: tone.fg, background: tone.bg, borderColor: tone.bd }}
    >
      <MtIcon type={type} />
      {type}
    </span>
  )
}

// ── sparkline ────────────────────────────────────────────────────────────────

function Spark({ series, color }: { series: number[]; color: string }) {
  const w = 60, h = 22
  if (series.length === 0) return <svg width={w} height={h} />
  const min = Math.min(...series), max = Math.max(...series)
  const span = max - min || 1
  const pts = series.map((v, i) =>
    `${(i / Math.max(1, series.length - 1)) * w},${h - ((v - min) / span) * (h - 2) - 1}`
  ).join(' ')
  return (
    <svg width={w} height={h} className="flex-none overflow-visible">
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.3" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

// ── chart ────────────────────────────────────────────────────────────────────

function Chart({ metric, bucketed, traces }: { metric: MetricSeries; bucketed: BucketedSeries; traces: TraceOverlay[] }) {
  const W = 920, H = 280, P = { l: 56, r: 16, t: 24, b: 36 }
  const cw = W - P.l - P.r, ch = H - P.t - P.b
  const series = metric.type === 'histogram'
    ? [bucketed.p50!, bucketed.p95!, bucketed.p99!]
    : [bucketed.values]
  const n = series[0].length
  const all = series.flat()
  const min = Math.min(0, ...all)
  const max = Math.max(...all) * 1.08 || 1
  const span = max - min || 1

  const xAt = (i: number) => P.l + (i / Math.max(1, n - 1)) * cw
  const yAt = (v: number) => P.t + ch - ((v - min) / span) * ch

  const ticks: number[] = []
  for (let i = 0; i <= 4; i++) ticks.push(min + (span / 4) * i)

  const accent = svcColor(metric.service_name).fg
  const COLORS = metric.type === 'histogram' ? ['#7aa3c5', accent, '#9a3b3b'] : [accent]
  const LABELS = metric.type === 'histogram' ? ['p50', 'p95', 'p99'] : ['value']

  const linePath = (s: number[]) =>
    s.map((v, i) => `${i === 0 ? 'M' : 'L'} ${xAt(i)} ${yAt(v)}`).join(' ')
  const areaPath = (s: number[]) =>
    linePath(s) + ` L ${xAt(n - 1)} ${yAt(min)} L ${xAt(0)} ${yAt(min)} Z`

  const [hoverI, setHoverI] = useState<number | null>(null)

  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${W} ${H}`} width="100%" height={H}
        className="block cursor-crosshair"
        onMouseLeave={() => setHoverI(null)}
        onMouseMove={(e) => {
          const rect = (e.currentTarget as SVGSVGElement).getBoundingClientRect()
          const px = ((e.clientX - rect.left) / rect.width) * W
          const ratio = Math.max(0, Math.min(1, (px - P.l) / cw))
          setHoverI(Math.round(ratio * (n - 1)))
        }}
      >
        {ticks.map((v, i) => (
          <g key={i}>
            <line x1={P.l} x2={W - P.r} y1={yAt(v)} y2={yAt(v)}
              stroke="var(--border)" strokeWidth="1"
              strokeDasharray={i === 0 ? '0' : '2 4'} />
            <text x={P.l - 8} y={yAt(v) + 3} textAnchor="end"
              fontFamily="var(--font-mono)" fontSize="10" fill="var(--muted-foreground)">
              {fmtVal(v, metric.unit)}
            </text>
          </g>
        ))}

        {series.length === 1 ? (
          <g>
            <path d={areaPath(series[0])} fill={COLORS[0]} opacity="0.12" />
            <path d={linePath(series[0])} fill="none" stroke={COLORS[0]} strokeWidth="2"
              strokeLinejoin="round" strokeLinecap="round" />
          </g>
        ) : (
          series.map((s, i) => (
            <path key={i} d={linePath(s)} fill="none"
              stroke={COLORS[i]}
              strokeWidth={i === series.length - 1 ? 2.4 : 1.6}
              opacity={i === 0 ? 0.7 : 1}
              strokeLinejoin="round" strokeLinecap="round" />
          ))
        )}

        {/* Trace overlay markers — vertical dotted lines at each in-window
            trace's start. Red dot for error/slow, neutral otherwise. The
            xPosForTrace helper handles clamping + empty-points cases. */}
        {traces.map((t) => {
          const bin = xPosForTrace(t, metric.points, n)
          if (bin == null) return null
          const x = xAt(bin)
          const isError = t.status_code === 2
          const isSlow  = t.duration_ns >= 250_000_000 // 250ms
          const tone = isError || isSlow ? 'var(--danger)' : 'var(--ink3)'
          return (
            <g key={t.trace_id} data-testid="metric-trace-marker">
              <line x1={x} x2={x} y1={P.t} y2={H - P.b}
                stroke={tone} strokeWidth="1" strokeDasharray="2 3" opacity="0.55" />
              <circle cx={x} cy={P.t - 4} r="2.5" fill={tone} />
              <title>{`${t.op} · ${t.trace_id.slice(0, 8)}`}</title>
            </g>
          )
        })}

        {hoverI != null && (
          <g>
            <line x1={xAt(hoverI)} x2={xAt(hoverI)} y1={P.t} y2={H - P.b}
              stroke="var(--foreground)" strokeWidth="1" />
            {series.map((s, i) => (
              <circle key={i} cx={xAt(hoverI)} cy={yAt(s[hoverI])} r="3.5"
                fill="var(--background)" stroke={COLORS[i]} strokeWidth="2" />
            ))}
          </g>
        )}

        <line x1={P.l} y1={P.t} x2={P.l} y2={H - P.b} stroke="var(--border)" strokeWidth="1" />
      </svg>

      {hoverI != null && (
        <div
          className="absolute top-[30px] bg-foreground text-background font-mono text-[11px] px-2.5 py-2 rounded-md min-w-[160px] pointer-events-none shadow-[0_8px_18px_-8px_rgba(0,0,0,.32)]"
          style={{ left: `calc(${(xAt(hoverI) / W) * 100}% + 14px)` }}
        >
          {series.map((s, i) => (
            <div key={i} className="flex gap-2 items-baseline py-px">
              <span className="w-2 h-2 rounded-lg inline-block" style={{ background: COLORS[i] }} />
              <span className="flex-1 opacity-70">{LABELS[i]}</span>
              <span className="font-bold">{fmtVal(s[hoverI], metric.unit)}</span>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center gap-[18px] pt-2.5 px-4 flex-wrap">
        {LABELS.map((L, i) => (
          <span key={i} className="inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
            <span className="w-[18px] h-[2.4px]" style={{ background: COLORS[i] }} />
            {L}
          </span>
        ))}
      </div>
    </div>
  )
}

// ── stat strip ───────────────────────────────────────────────────────────────

function StatBox({ s }: { s: Stat }) {
  return (
    <div className="px-[18px] py-3.5 border-r border-border">
      <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em]">{s.label}</div>
      <div
        className="text-[26px] font-semibold leading-[1.05] mt-1"
        style={{
          fontFamily: 'var(--font-serif, serif)',
          color: s.tone === 'danger' ? '#9a3b3b' : s.tone === 'ok' ? '#3e6a3e' : 'var(--foreground)',
        }}
      >{s.value}</div>
      {s.sub && (
        <div className="font-mono text-[10px] text-muted-foreground mt-1">{s.sub}</div>
      )}
    </div>
  )
}

// ── page ─────────────────────────────────────────────────────────────────────

export default function Metrics() {
  const [catalog, setCatalog] = useState<MetricCatalogEntry[]>([])
  const [selected, setSelected] = useState<{ name: string; service: string } | null>(null)
  const [series, setSeries] = useState<MetricSeries | null>(null)
  const [query, setQuery] = useState('')
  const [typeSel, setTypeSel] = useState<MetricType | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancel = false
    api.metrics.list().then(r => {
      if (cancel) return
      setCatalog(r.data ?? [])
      setLoading(false)
      // auto-select first metric
      if (!selected && (r.data ?? []).length > 0) {
        const m = r.data[0]
        setSelected({ name: m.name, service: m.service_name })
      }
    }).catch(() => { if (!cancel) setLoading(false) })
    return () => { cancel = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!selected) return
    let cancel = false
    api.metrics.series({ name: selected.name, service: selected.service, withTraces: true })
      .then(r => { if (!cancel) setSeries(r.data) })
      .catch(() => { if (!cancel) setSeries(null) })
    return () => { cancel = true }
  }, [selected])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return catalog.filter(m => {
      if (typeSel && m.type !== typeSel) return false
      if (!q) return true
      return (m.name + ' ' + m.service_name + ' ' + (m.description || '')).toLowerCase().includes(q)
    })
  }, [catalog, query, typeSel])

  const groups = useMemo(() => {
    const out: Record<string, MetricCatalogEntry[]> = {}
    for (const m of filtered) (out[m.service_name] = out[m.service_name] || []).push(m)
    return out
  }, [filtered])

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">
        Loading metrics…
      </div>
    )
  }

  if (catalog.length === 0) {
    return (
      <EmptyState
        title="No metrics yet"
        hint={<>Point your OTLP exporter at <code>localhost:4318/v1/metrics</code> and spaniel will collect gauges, counters, and histograms here.</>}
        glyph={
          <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
            <rect x="4" y="20" width="6" height="8" rx="1" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
            <rect x="13" y="12" width="6" height="16" rx="1" stroke="currentColor" strokeWidth="1.5" opacity="0.6" />
            <rect x="22" y="6" width="6" height="22" rx="1" stroke="currentColor" strokeWidth="1.5" opacity="0.7" />
          </svg>
        }
      />
    )
  }

  return (
    <div className="flex-1 flex min-h-0 overflow-hidden">
      {/* left rail */}
      <div className="w-[320px] border-r border-border bg-surface flex flex-col overflow-hidden">
        <div className="px-3 py-2.5 border-b border-border flex flex-col gap-2">
          <span className="inline-flex items-center gap-[7px] bg-muted border border-border rounded-md px-2.5 h-7">
            <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
              <circle cx="6" cy="6" r="4" stroke="var(--muted-foreground)" strokeWidth="1.4" />
              <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="var(--muted-foreground)" strokeWidth="1.4" strokeLinecap="round" />
            </svg>
            <input value={query} onChange={e => setQuery(e.target.value)}
              placeholder="search metrics…"
              className="flex-1 border-none outline-none bg-transparent font-mono text-[11.5px] text-foreground" />
          </span>
          <div className="flex gap-1.5 flex-wrap">
            {(['gauge', 'counter', 'histogram'] as const).map(t => {
              const on = typeSel === t
              return (
                <button
                  key={t}
                  type="button"
                  onClick={() => setTypeSel(on ? null : t)}
                  className={`cursor-pointer outline-none px-[9px] py-[3px] rounded-[5px] font-mono text-[10px] font-semibold tracking-[0.06em] uppercase border border-border inline-flex items-center gap-[5px] ${on ? 'bg-[var(--accent,var(--muted))] text-[var(--accent-foreground,var(--foreground))]' : 'bg-muted text-muted-foreground'}`}
                >
                  <MtIcon type={t} />{t}
                </button>
              )
            })}
            <span className="flex-1" />
            <span className="font-mono text-[10px] text-muted-foreground self-center">
              {filtered.length} metrics
            </span>
          </div>
        </div>

        <div className="flex-1 overflow-x-hidden overflow-y-auto">
          {Object.entries(groups).map(([svc, list]) => {
            const c = svcColor(svc).fg
            return (
              <div key={svc}>
                <div className="pt-2.5 px-3 pb-1 font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] bg-surface sticky top-0 flex items-center gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-md" style={{ background: c }} />
                  {svc}
                  <span className="flex-1" />
                  <span className="text-foreground">{list.length}</span>
                </div>
                {list.map(m => {
                  const isSel = selected?.name === m.name && selected?.service === m.service_name
                  return (
                    <button
                      key={m.service_name + '/' + m.name}
                      type="button"
                      onClick={() => setSelected({ name: m.name, service: m.service_name })}
                      className={`w-full px-3 py-2 border-none cursor-pointer outline-none text-left border-b border-border grid gap-2.5 items-center border-l-2 ${isSel ? 'bg-muted' : 'bg-transparent border-l-transparent'}`}
                      style={{
                        gridTemplateColumns: 'minmax(0,1fr) 60px',
                        borderLeftColor: isSel ? c : undefined,
                      }}
                    >
                      <div className="min-w-0">
                        <div className="font-mono text-[11px] font-semibold text-foreground overflow-hidden text-ellipsis whitespace-nowrap">{m.name}</div>
                        <div className="mt-[3px]">
                          <MetricKindTag type={m.type} />
                        </div>
                      </div>
                      <span className="font-mono text-[10px] text-muted-foreground text-right">{m.sample_count} pts</span>
                    </button>
                  )
                })}
              </div>
            )
          })}
        </div>
      </div>

      {/* main panel */}
      <div className="flex-1 overflow-x-hidden overflow-y-auto flex flex-col">
        {series && selected ? <MainPanel series={series} /> : (
          <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">Select a metric</div>
        )}
      </div>
    </div>
  )
}

function MainPanel({ series }: { series: MetricSeries }) {
  const bucketed = useMemo(() => bucketPoints(series.points, series.type), [series])
  const stats = useMemo(() => statsFor(series, bucketed), [series, bucketed])

  return (
    <>
      <div className="pt-[18px] px-6 pb-3.5 border-b border-border flex items-start gap-3.5">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2.5">
            <span
              className="font-mono text-[10px] font-semibold px-[7px] py-px rounded-[4px]"
              style={{
                color: svcColor(series.service_name).fg,
                background: svcColor(series.service_name).bg,
              }}
            >{series.service_name}</span>
            <MetricKindTag type={series.type} />
            {series.unit && (
              <span className="px-[7px] py-0.5 rounded-[5px] bg-muted text-muted-foreground border border-border font-mono text-[10px] font-semibold">unit · {series.unit}</span>
            )}
          </div>
          <h1 className="mx-0 mt-2 mb-1 font-mono text-xl font-bold tracking-[-0.01em] text-foreground break-all">{series.name}</h1>
          {series.description && (
            <div
              className="italic text-[13px] text-muted-foreground max-w-[720px]"
              style={{ fontFamily: 'var(--font-serif, serif)' }}
            >{series.description}</div>
          )}
        </div>
      </div>

      <div className="grid grid-cols-4 border-b border-border">
        {stats.map((s, i) => <StatBox key={i} s={s} />)}
      </div>

      <div className="pt-[18px] px-4 pb-[22px]">
        <Chart metric={series} bucketed={bucketed} traces={series.traces ?? []} />
      </div>

      <div className="pt-1 px-6 pb-[22px] font-mono text-[11px] text-muted-foreground">
        {series.points.length} raw points · bucketed into 60 bins
      </div>

      <CorrelatedTracesPanel series={series} bucketed={bucketed} />
    </>
  )
}

// ── correlated-traces panel ──────────────────────────────────────────────────

function CorrelatedTracesPanel({ series, bucketed }: { series: MetricSeries; bucketed: BucketedSeries }) {
  const navigate = useNavigate()
  const traces = series.traces ?? []
  if (traces.length === 0) return null

  // For value-at-time, prefer the histogram p95 series when present; fall
  // back to the gauge/counter value series. Same source the chart line uses.
  const valueSeries = series.type === 'histogram'
    ? (bucketed.p95 ?? bucketed.values)
    : bucketed.values
  const bins = valueSeries.length

  return (
    <div className="border-t border-[var(--border)] px-6 py-4">
      <div className="mb-2 font-mono text-[10px] uppercase tracking-[0.14em] text-[var(--muted-foreground)]">
        Traces during this window
        <span className="ml-2 normal-case tracking-normal text-[var(--ink2)]">{traces.length}</span>
      </div>
      <div
        data-testid="correlated-traces"
        className="grid gap-y-1 font-mono text-[11px]"
        style={{ gridTemplateColumns: '14px 56px 1fr auto auto auto' }}
      >
        {traces.map(t => {
          const bin   = xPosForTrace(t, series.points, bins)
          const v     = valueAtBin(valueSeries, bin)
          const offs  = relativeOffset(t.start_ns, series.points)
          const isErr = t.status_code === 2
          const isSlow = t.duration_ns >= 250_000_000
          return (
            <div key={t.trace_id} className="contents">
              <span
                className={`size-1.5 self-center rounded-full ${isErr || isSlow ? 'bg-[var(--danger)]' : 'bg-[var(--ink3)]'}`}
                aria-hidden
              />
              <span className="text-[var(--ink3)]">{offs}</span>
              <span className="truncate font-bold text-[var(--ink)]">{t.op}</span>
              <span className="text-[var(--ink3)]">{t.trace_id.slice(0, 8)}…</span>
              <span className="text-[var(--ink2)]">{v != null ? fmtVal(v, series.unit) : '—'}</span>
              <button
                type="button"
                onClick={() => navigate(`/traces/${t.trace_id}`)}
                className="cursor-pointer rounded border border-transparent px-1.5 text-[var(--accent)] hover:border-[var(--border)]"
              >
                open →
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// relativeOffset returns "−12m" / "−3s" / "now" for a trace start vs the
// last point in the series — the mockup's "Xm ago" column.
function relativeOffset(startNs: number, points: { timestamp_ns: number }[]): string {
  if (points.length === 0) return ''
  const lastNs = points[points.length - 1].timestamp_ns
  const deltaMs = (lastNs - startNs) / 1_000_000
  if (deltaMs < 1_000) return 'now'
  if (deltaMs < 60_000) return `−${Math.round(deltaMs / 1000)}s`
  if (deltaMs < 3_600_000) return `−${Math.round(deltaMs / 60_000)}m`
  return `−${Math.round(deltaMs / 3_600_000)}h`
}
