import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, TraceRow } from '../lib/api'
import { createWS, SpanEvent } from '../lib/ws'

function StatusBadge({ code }: { code: number }) {
  const variant = code === 2 ? 'destructive' : 'secondary'
  const label = code === 2 ? 'ERROR' : 'OK'
  return <Badge variant={variant} className="font-mono text-[10px]">{label}</Badge>
}

function fmtDuration(ns: number): string {
  if (ns < 1_000) return `${ns}ns`
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

function fmtTs(ns: number): string {
  return new Date(ns / 1_000_000).toLocaleTimeString()
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

  return (
    <div className="fade-up flex h-full flex-col items-center justify-center gap-7 overflow-y-auto px-6 py-8">
      <SpanielWaiting />

      <div className="text-center">
        <h2 className="mb-2.5 text-xl font-semibold tracking-tight text-foreground">
          Waiting for traces…
        </h2>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <EndpointPill port="4318" proto="HTTP/OTLP" />
          <EndpointPill port="4317" proto="gRPC/OTLP" />
        </div>
      </div>

      <div className="w-full max-w-[520px]">
        <p className="mb-2 text-[10px] font-bold uppercase tracking-widest text-muted-foreground">
          Step 1 — point your app at spaniel
        </p>
        <pre className="m-0 overflow-x-auto rounded-lg border border-[var(--ok)] bg-[var(--ok-bg)] px-4 py-3 font-mono text-xs leading-relaxed text-[var(--ok-ink)]">
          {`export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318\nexport OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`}
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

export default function TraceList() {
  const [traces, setTraces] = useState<TraceRow[]>([])
  const [services, setServices] = useState<string[]>([])
  const [searchParams] = useSearchParams()
  const [filterService, setFilterService] = useState(searchParams.get('service') ?? 'all')
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()
  const tracesRef = useRef(traces)
  tracesRef.current = traces

  useEffect(() => {
    api.traces.list().then(r => {
      setTraces(r.data ?? [])
      setLoading(false)
    }).catch(() => setLoading(false))

    api.services.list().then(r => setServices(r.data ?? []))

    const disconnect = createWS((ev: SpanEvent) => {
      if (ev.type !== 'span') return
      const newRow: TraceRow = {
        trace_id: ev.traceId,
        service_name: ev.serviceName,
        name: ev.name,
        status_code: ev.statusCode,
        start_ns: Date.now() * 1_000_000,
        end_ns: (Date.now() * 1_000_000) + ev.durationNs,
        duration_ns: ev.durationNs,
        session_id: '',
        has_n1: false,
      }
      setTraces(prev => {
        if (prev.some(t => t.trace_id === ev.traceId)) return prev
        return [newRow, ...prev].slice(0, 200)
      })
    })
    return disconnect
  }, [])

  const filtered = filterService === 'all'
    ? traces
    : traces.filter(t => t.service_name === filterService)

  if (loading) {
    return <div className="p-8 text-muted-foreground text-sm">Loading…</div>
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-3 px-6 py-4 border-b border-border">
        <h1 className="text-sm font-medium flex-1">Traces</h1>
        {services.length > 0 && (
          <Select value={filterService} onValueChange={v => setFilterService(v ?? 'all')}>
            <SelectTrigger className="w-40 h-8 text-xs">
              <SelectValue placeholder="All services" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All services</SelectItem>
              {services.map(s => <SelectItem key={s} value={s}>{s}</SelectItem>)}
            </SelectContent>
          </Select>
        )}
      </div>

      {filtered.length === 0 ? (
        <EmptyState />
      ) : (
        <div className="overflow-auto flex-1">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Service</TableHead>
                <TableHead>Root Span</TableHead>
                <TableHead className="text-right">Duration</TableHead>
                <TableHead className="text-right">Time</TableHead>
                <TableHead className="text-center w-20">Status</TableHead>
                <TableHead className="text-center w-16">Issues</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map(t => (
                <TableRow
                  key={t.trace_id}
                  onClick={() => navigate(`/traces/${t.trace_id}`)}
                  className="cursor-pointer"
                >
                  <TableCell className="text-muted-foreground text-xs">{t.service_name}</TableCell>
                  <TableCell className="font-mono text-xs">{t.name}</TableCell>
                  <TableCell className="text-right font-mono text-xs text-muted-foreground">
                    {fmtDuration(t.duration_ns)}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {fmtTs(t.start_ns)}
                  </TableCell>
                  <TableCell className="text-center">
                    <StatusBadge code={t.status_code} />
                  </TableCell>
                  <TableCell className="text-center w-16">
                    {t.has_n1 && (
                      <span style={{
                        display: 'inline-flex', alignItems: 'center', gap: 3,
                        padding: '2px 6px', borderRadius: 4,
                        fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 700,
                        letterSpacing: '0.04em', textTransform: 'uppercase',
                        color: 'var(--warn-ink)', background: 'var(--warn-bg)',
                      }}>N+1</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
