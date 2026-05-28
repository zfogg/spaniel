import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { DurBar } from '@/components/DurBar'
import { api, TraceRow, Session } from '../lib/api'
import { createWS } from '../lib/ws'
import { traceTag } from '../lib/trace-tag'
import { fmtRelative } from '../lib/fmt-relative'
import { SEARCH_PALETTE_EVENT } from '../lib/shortcuts'

const GRID_COLS = 'minmax(0,1fr) 90px 80px 280px 70px'
const SLOW_NS = 250_000_000
const MAX_SESSIONS = 7
const MAX_SERVICES = 15

function fmtDuration(ns: number): string {
  if (ns < 1_000) return `${ns}ns`
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

// ── Empty state ───────────────────────────────────────────────────────────────

type Lang = 'python' | 'nodejs' | 'go' | 'java'

const LANG_LABELS: Record<Lang, string> = {
  python: 'Python',
  nodejs: 'Node.js',
  go: 'Go',
  java: 'Java',
}

const LANG_SNIPPETS: Record<Lang, string> = {
  python:
`pip install opentelemetry-distro opentelemetry-exporter-otlp
opentelemetry-bootstrap -a install
opentelemetry-instrument python app.py`,

  nodejs:
`npm install @opentelemetry/api \\
  @opentelemetry/auto-instrumentations-node

# add to your start command:
node -r @opentelemetry/auto-instrumentations-node/register app.js`,

  go:
`go get go.opentelemetry.io/otel \\
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp

// wrap your handler:
handler = otelhttp.NewHandler(handler, "my-service")`,

  java:
`# download the javaagent:
curl -LO https://github.com/open-telemetry/opentelemetry-java-instrumentation/\\
  releases/latest/download/opentelemetry-javaagent.jar

# run your app:
java -javaagent:opentelemetry-javaagent.jar -jar app.jar`,
}

function SpanielWaiting() {
  return (
    <div className="relative size-[76px] shrink-0">
      <span className="ping absolute inset-[-2px] block rounded-full border-2 border-[var(--ok)]" />
      <span className="ping absolute inset-[-2px] block rounded-full border-2 border-[var(--ok)] [animation-delay:0.8s]" />
      <svg width={76} height={76} viewBox="0 0 76 76" className="block">
        <ellipse cx="20" cy="36" rx="14" ry="23" fill="var(--accent)" opacity="0.72" transform="rotate(-13 20 36)" />
        <ellipse cx="56" cy="36" rx="14" ry="23" fill="var(--accent)" opacity="0.46" transform="rotate(13 56 36)" />
        <circle cx="38" cy="38" r="19" fill="var(--surface)" stroke="var(--ink)" strokeWidth="1.4" />
        <circle cx="32" cy="36" r="2.3" fill="var(--ink)" />
        <circle cx="44" cy="36" r="2.3" fill="var(--ink)" />
        <circle cx="33.2" cy="35.2" r="0.7" fill="var(--surface)" />
        <circle cx="45.2" cy="35.2" r="0.7" fill="var(--surface)" />
        <ellipse cx="38" cy="42" rx="2.8" ry="2" fill="var(--ink2)" />
        <path d="M33.5 46.5 Q38 50 42.5 46.5" stroke="var(--ink)" strokeWidth="1.4" fill="none" strokeLinecap="round" />
      </svg>
    </div>
  )
}

function EndpointPill({ port, proto }: { port: string; proto: string }) {
  return (
    <div className="flex items-center gap-1.5 rounded-md border border-[var(--ok)] bg-[var(--ok-bg)] px-2.5 py-1">
      <span className="pulse-dot inline-block size-1.5 shrink-0 rounded-full bg-[var(--ok)]" />
      <span className="font-mono text-[11px] text-[var(--ok-ink)]">:{port}</span>
      <span className="font-sans text-[10px] text-[var(--ok-ink)] opacity-70">{proto}</span>
    </div>
  )
}

function EmptyState() {
  const [lang, setLang] = useState<Lang>('python')
  // Pull live OTLP ports from /api/settings so the empty state reflects the
  // actually-bound ports, not the hardcoded defaults. The runtime.* fields
  // are the bound ports (which may differ from the configured ones if the
  // user changed the config without restarting yet).
  const [ports, setPorts] = useState({ http: 4318, grpc: 4317 })
  useEffect(() => {
    api.settings.get().then(r => {
      const rt = r.data?.runtime
      if (rt) setPorts({
        http: rt.otlp_http_port || 4318,
        grpc: rt.otlp_grpc_port || 4317,
      })
    }).catch(() => { /* keep defaults */ })
  }, [])

  return (
    <div className="fade-up flex h-full flex-col items-center justify-center gap-7 overflow-y-auto px-6 py-8">
      <SpanielWaiting />

      <div className="text-center">
        <h2 className="mb-2.5 text-xl font-semibold tracking-tight text-foreground">
          Waiting for traces…
        </h2>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <EndpointPill port={String(ports.http)} proto="HTTP/OTLP" />
          <EndpointPill port={String(ports.grpc)} proto="gRPC/OTLP" />
        </div>
      </div>

      <div className="w-full max-w-[520px]">
        <p className="mb-2 text-[10px] font-bold uppercase tracking-widest text-muted-foreground">
          Step 1 — point your app at spaniel
        </p>
        <pre className="m-0 overflow-x-auto rounded-lg border border-[var(--ok)] bg-[var(--ok-bg)] px-4 py-3 font-mono text-xs leading-relaxed text-[var(--ok-ink)]">
          {`export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:${ports.http}\nexport OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`}
        </pre>
      </div>

      <div className="w-full max-w-[520px]">
        <p className="mb-2 text-[10px] font-bold uppercase tracking-widest text-muted-foreground">
          Step 2 — install the SDK
        </p>
        <div className="mb-1.5 flex gap-0.5 rounded-lg bg-muted p-0.5">
          {(Object.keys(LANG_LABELS) as Lang[]).map(l => (
            <button
              key={l}
              type="button"
              onClick={() => setLang(l)}
              className={[
                'flex-1 cursor-pointer rounded-md border-0 py-1 text-[11px] font-medium transition-colors',
                lang === l
                  ? 'bg-background text-foreground shadow-sm'
                  : 'bg-transparent text-muted-foreground',
              ].join(' ')}
            >
              {LANG_LABELS[l]}
            </button>
          ))}
        </div>
        <pre className="m-0 overflow-x-auto whitespace-pre rounded-lg border border-border bg-card px-4 py-3 font-mono text-[11.5px] leading-relaxed text-foreground">
          {LANG_SNIPPETS[lang]}
        </pre>
      </div>

      <p className="m-0 max-w-sm text-center text-[11px] text-muted-foreground">
        Traces appear here in real time. Your first trace should arrive within seconds.
      </p>
    </div>
  )
}

// ── Tag chip ──────────────────────────────────────────────────────────────────

type TagTone = 'danger' | 'warn' | 'accent'

const TAG_TONE: Record<NonNullable<ReturnType<typeof traceTag>>, TagTone> = {
  'n+1':     'danger',
  'slow':    'warn',
  'baseline': 'accent',
  'error':   'danger',
}

const TONE_CLASS: Record<TagTone, string> = {
  danger: 'bg-[var(--danger-bg)] text-[var(--danger-ink)]',
  warn:   'bg-[var(--warn-bg)]   text-[var(--warn-ink)]',
  accent: 'bg-[color-mix(in_oklch,var(--accent)_15%,transparent)] text-[var(--accent)]',
}

const ISSUE_ABBREV: Record<string, string> = {
  slow_db: 'slow db', chatty_http: 'chatty', large_payload: 'large',
  cache_miss_storm: 'cache', tracing_gap: 'gap', error_chain: 'err chain',
  serial_promise: 'serial', synchronous_io: 'sync io',
}

function IssueKindChip({ kind }: { kind: string }) {
  const label = ISSUE_ABBREV[kind] ?? kind.replace(/_/g, ' ')
  return (
    <span className="inline-flex items-center px-1.5 py-px rounded font-mono text-[9px] font-bold uppercase tracking-[0.04em] bg-[var(--warn-bg)] text-[var(--warn-ink)]">
      {label}
    </span>
  )
}

function TagChip({ tag }: { tag: NonNullable<ReturnType<typeof traceTag>> }) {
  const tone = TAG_TONE[tag]
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0 font-mono text-[9px] font-bold uppercase tracking-wide ${TONE_CLASS[tone]}`}>
      {tag}
    </span>
  )
}

// ── Trace row ─────────────────────────────────────────────────────────────────

function TraceRowItem({
  trace,
  maxNs,
  isFirst,
  baselineSessionId,
  onClick,
}: {
  trace: TraceRow
  maxNs: number
  isFirst: boolean
  baselineSessionId: string | null
  onClick: () => void
}) {
  const tag = traceTag(trace, baselineSessionId)
  const hot = tag === 'n+1' || tag === 'slow'

  return (
    <div
      data-testid={`trace-row-${trace.trace_id}`}
      onClick={onClick}
      className="grid cursor-pointer items-center gap-0 px-[18px] py-[10px] transition-colors hover:bg-[color-mix(in_oklch,var(--accent)_5%,transparent)] border-b border-b-[var(--line2)]"
      style={{
        gridTemplateColumns: GRID_COLS,
        background: isFirst
          ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
          : undefined,
        borderLeft: isFirst ? '2px solid var(--accent)' : '2px solid transparent',
      }}
    >
      {/* operation + tag chips + trace id */}
      <div className="min-w-0">
        <div className="mb-0.5 flex items-center gap-2">
          <span className="truncate font-mono text-[12px] font-semibold text-[var(--ink)]">
            {trace.name}
          </span>
          {tag && <TagChip tag={tag} />}
          {(trace.issue_kinds ?? []).filter(k => k !== 'n_plus_one').slice(0, 2).map(k => (
            <IssueKindChip key={k} kind={k} />
          ))}
        </div>
        <div className="font-mono text-[10px] text-[var(--ink3)]">
          {trace.trace_id.slice(0, 16)}…
        </div>
      </div>

      {/* duration */}
      <div className="text-right font-mono text-[12px] font-semibold text-[var(--ink)]">
        {fmtDuration(trace.duration_ns)}
      </div>

      {/* span count */}
      <div className="text-right font-mono text-[11px] text-[var(--ink2)]">
        {trace.span_count}
      </div>

      {/* shape bar */}
      <div className="pl-3">
        <DurBar durNs={trace.duration_ns} maxNs={maxNs} hot={hot} />
      </div>

      {/* relative time */}
      <div className="text-right font-mono text-[10px] text-[var(--ink3)]">
        {fmtRelative(trace.start_ns)}
      </div>
    </div>
  )
}

// ── Sidebar (filter rail) ───────────────────────────────────────────────────────

function SbGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-2 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--ink3)]">
        {title}
      </div>
      <div className="flex flex-col gap-0.5">{children}</div>
    </div>
  )
}

function SbItem({
  children,
  active,
  count,
  dot,
  onClick,
}: {
  children: React.ReactNode
  active?: boolean
  count?: number | string
  dot?: string
  onClick?: () => void
}) {
  return (
    <div
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      className={[
        'flex select-none items-center gap-2 rounded-md px-2 py-[5px] text-xs',
        onClick ? 'cursor-pointer' : 'cursor-default',
        active
          ? 'bg-[var(--surface)] font-semibold text-[var(--ink)] shadow-[inset_0_0_0_1px_var(--line)]'
          : 'bg-transparent font-medium text-[var(--ink2)]',
      ].join(' ')}
    >
      {dot && <span className="size-[7px] shrink-0 rounded-full" style={{ background: dot }} />}
      <span className="flex-1 truncate">{children}</span>
      {count != null && <span className="font-mono text-[10px] text-[var(--ink3)]">{count}</span>}
    </div>
  )
}

function SbMore({ count }: { count: number }) {
  return (
    <div className="flex select-none items-center gap-2 px-2 py-[5px] font-mono text-[11px] text-[var(--ink3)]">
      <span className="tracking-[0.25em]">…</span>
      <span className="flex-1">+{count} more</span>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

type QuickFilter = 'lint' | 'slow' | 'errors'

export default function TraceList() {
  const [traces, setTraces] = useState<TraceRow[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [searchParams] = useSearchParams()
  const [filterService, setFilterService] = useState(searchParams.get('service') ?? 'all')
  const [filterSession, setFilterSession] = useState<string | null>(null)
  const [quickFilter, setQuickFilter] = useState<QuickFilter | null>(null)
  const [loading, setLoading] = useState(true)
  const [baselineSessionId, setBaselineSessionId] = useState<string | null>(null)
  const navigate = useNavigate()
  const tracesRef = useRef(traces)
  tracesRef.current = traces

  useEffect(() => {
    api.traces.list().then(r => {
      setTraces(r.data ?? [])
      setLoading(false)
    }).catch(() => setLoading(false))

    api.sessions.list().then(r => {
      const list = r.data ?? []
      setSessions(list)
      const baseline = list.find(s => s.is_baseline)
      if (baseline) setBaselineSessionId(baseline.id)
    })

    const disconnect = createWS((ev) => {
      if (ev.type !== 'span') return
      const p = ev.payload
      const newRow: TraceRow = {
        trace_id: p.traceId,
        service_name: p.serviceName,
        name: p.name,
        status_code: p.statusCode,
        start_ns: Date.now() * 1_000_000,
        end_ns: (Date.now() * 1_000_000) + p.durationNs,
        duration_ns: p.durationNs,
        session_id: '',
        session_label: '',
        has_n1: false,
        span_count: 1,
      }
      setTraces(prev => {
        if (prev.some(t => t.trace_id === p.traceId)) return prev
        return [newRow, ...prev].slice(0, 200)
      })
    })
    return disconnect
  }, [])

  // Per-service trace counts + most-recent timestamp, derived from the
  // loaded traces. Services are ordered by recency (newest trace first).
  const serviceCounts: Record<string, number> = {}
  const serviceLatest: Record<string, number> = {}
  for (const t of traces) {
    serviceCounts[t.service_name] = (serviceCounts[t.service_name] ?? 0) + 1
    serviceLatest[t.service_name] = Math.max(serviceLatest[t.service_name] ?? 0, t.start_ns)
  }
  const serviceNames = Object.keys(serviceCounts).sort((a, b) => serviceLatest[b] - serviceLatest[a])
  const sessionsByRecent = [...sessions].sort((a, b) => b.created_at - a.created_at)

  const lintCount = traces.filter(t => t.has_n1).length
  const slowCount = traces.filter(t => t.duration_ns > SLOW_NS).length
  const errorCount = traces.filter(t => t.status_code === 2).length

  const matchesQuick = (t: TraceRow) =>
    quickFilter === 'lint' ? t.has_n1
    : quickFilter === 'slow' ? t.duration_ns > SLOW_NS
    : quickFilter === 'errors' ? t.status_code === 2
    : true

  const filtered = traces.filter(t =>
    (filterService === 'all' || t.service_name === filterService) &&
    (filterSession === null || t.session_id === filterSession) &&
    matchesQuick(t),
  )

  const maxNs = filtered.reduce((m, t) => Math.max(m, t.duration_ns), 0)

  const toggle = <T,>(cur: T, val: T, set: (v: T) => void, reset: T) =>
    set(cur === val ? reset : val)

  if (loading) {
    return <div className="p-8 text-muted-foreground text-sm">Loading…</div>
  }

  if (traces.length === 0) {
    return (
      <div className="flex h-full flex-col">
        <EmptyState />
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0">
      {/* filter rail */}
      <aside className="flex w-[200px] shrink-0 flex-col gap-[18px] overflow-y-auto border-r border-[var(--line)] bg-[var(--surface2)] px-[14px] py-[18px] font-sans">
        {sessionsByRecent.length > 0 && (
          <SbGroup title="sessions">
            {sessionsByRecent.slice(0, MAX_SESSIONS).map(s => (
              <SbItem
                key={s.id}
                active={filterSession === s.id}
                dot={s.is_baseline ? 'var(--ink3)' : 'var(--accent)'}
                count={s.trace_count}
                onClick={() => toggle(filterSession, s.id, setFilterSession, null)}
              >
                {s.label}{s.is_baseline ? ' · baseline' : ''}
              </SbItem>
            ))}
            {sessionsByRecent.length > MAX_SESSIONS && (
              <SbMore count={sessionsByRecent.length - MAX_SESSIONS} />
            )}
          </SbGroup>
        )}

        {serviceNames.length > 0 && (
          <SbGroup title="services">
            {serviceNames.slice(0, MAX_SERVICES).map(name => (
              <SbItem
                key={name}
                active={filterService === name}
                count={serviceCounts[name]}
                onClick={() => toggle(filterService, name, setFilterService, 'all')}
              >
                {name}
              </SbItem>
            ))}
            {serviceNames.length > MAX_SERVICES && (
              <SbMore count={serviceNames.length - MAX_SERVICES} />
            )}
          </SbGroup>
        )}

        <SbGroup title="filters">
          <SbItem
            active={quickFilter === 'lint'}
            dot="var(--danger)"
            count={lintCount}
            onClick={() => toggle(quickFilter, 'lint', setQuickFilter, null)}
          >
            has lint warnings
          </SbItem>
          <SbItem
            active={quickFilter === 'slow'}
            dot="var(--warn)"
            count={slowCount}
            onClick={() => toggle(quickFilter, 'slow', setQuickFilter, null)}
          >
            {'> 250ms'}
          </SbItem>
          <SbItem
            active={quickFilter === 'errors'}
            dot="var(--accent)"
            count={errorCount}
            onClick={() => toggle(quickFilter, 'errors', setQuickFilter, null)}
          >
            errors
          </SbItem>
        </SbGroup>
      </aside>

      {/* main panel */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* panel head */}
        <div className="flex items-center gap-3 border-b border-[var(--line)] bg-[var(--surface)] px-4 py-3">
          <div className="flex-1">
            <div className="font-sans text-[13px] font-semibold tracking-[-0.01em] text-[var(--ink)]">
              Recent traces
            </div>
            <div className="mt-0.5 font-mono text-[10px] tracking-[0.02em] text-[var(--ink3)]">
              live · {filtered.length} of {traces.length} traces
            </div>
          </div>
          <button
            type="button"
            onClick={() => window.dispatchEvent(new CustomEvent(SEARCH_PALETTE_EVENT))}
            className="rounded-md border border-[var(--line)] bg-[var(--surface2)] px-2.5 py-[5px] font-mono text-[11px] text-[var(--ink2)]"
          >
            ⌘K
          </button>
        </div>

        {filtered.length === 0 ? (
          <div className="flex flex-1 items-center justify-center p-8 text-sm text-[var(--ink3)]">
            No traces match the current filters.
          </div>
        ) : (
          <div className="flex-1 overflow-auto">
            {/* column header */}
            <div
              className="grid border-b border-[var(--line)] bg-[var(--surface2)] px-[18px] py-2 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--ink3)]"
              style={{ gridTemplateColumns: GRID_COLS }}
            >
              <div>operation</div>
              <div className="text-right">dur</div>
              <div className="text-right">spans</div>
              <div className="pl-3">shape</div>
              <div className="text-right">ago</div>
            </div>

            {filtered.map((t, i) => (
              <TraceRowItem
                key={t.trace_id}
                trace={t}
                maxNs={maxNs}
                isFirst={i === 0}
                baselineSessionId={baselineSessionId}
                onClick={() => navigate(`/traces/${t.trace_id}`)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
