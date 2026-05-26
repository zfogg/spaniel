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

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-6 text-center py-24">
      <div className="flex items-center gap-2">
        <span className="w-2.5 h-2.5 rounded-full bg-success pulse-dot" />
        <span className="text-muted-foreground text-sm">Waiting for traces…</span>
      </div>
      <div className="bg-card border border-border rounded-lg p-6 text-left">
        <p className="text-muted-foreground text-xs mb-3">Point your app at spaniel:</p>
        <pre className="font-mono text-xs text-success leading-relaxed">
          {'OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318\nOTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf'}
        </pre>
      </div>
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
