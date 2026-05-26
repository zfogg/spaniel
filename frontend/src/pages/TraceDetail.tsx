import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { ScrollArea } from '@/components/ui/scroll-area'
import { api, Span, LintWarning } from '../lib/api'
import LintPanel from '../components/LintPanel'

function fmtDuration(ns: number) {
  if (ns < 1_000) return `${ns}ns`
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

function StatusBadge({ code }: { code: number }) {
  const variant = code === 2 ? 'destructive' : 'secondary'
  const label = code === 2 ? 'ERROR' : 'OK'
  return <Badge variant={variant} className="font-mono text-[10px]">{label}</Badge>
}

function SpanRow({
  span,
  minNs,
  totalNs,
  depth,
  onClick,
}: {
  span: Span
  minNs: number
  totalNs: number
  depth: number
  onClick: () => void
}) {
  const left = totalNs > 0 ? ((span.start_ns - minNs) / totalNs) * 100 : 0
  const width = totalNs > 0 ? Math.max(((span.end_ns - span.start_ns) / totalNs) * 100, 0.5) : 0.5
  const isError = span.status_code === 2

  return (
    <div
      onClick={onClick}
      className="flex items-center gap-2 px-4 py-1.5 hover:bg-muted/30 cursor-pointer border-b border-border/40 group"
    >
      <div className="w-48 flex-none truncate font-mono text-xs text-foreground/80" style={{ paddingLeft: depth * 14 }}>
        {span.name}
      </div>
      <div className="flex-1 relative h-4">
        <div className="absolute inset-y-0 w-full bg-muted/20 rounded" />
        <div
          className={`absolute inset-y-0 rounded ${isError ? 'bg-destructive/70' : 'bg-primary/60'} group-hover:opacity-80`}
          style={{ left: `${left}%`, width: `${width}%` }}
        />
      </div>
      <div className="w-20 text-right font-mono text-xs text-muted-foreground">
        {fmtDuration(span.end_ns - span.start_ns)}
      </div>
      <div className="w-20 text-right text-xs text-muted-foreground">{span.service_name}</div>
    </div>
  )
}

function SpanAttributesPanel({ span, open, onClose }: { span: Span | null; open: boolean; onClose: () => void }) {
  let attrs: Record<string, unknown> = {}
  let resource: Record<string, unknown> = {}
  if (span) {
    try { attrs = JSON.parse(span.attributes) } catch {}
    try { resource = JSON.parse(span.resource) } catch {}
  }

  return (
    <Sheet open={open} onOpenChange={(o: boolean) => !o && onClose()}>
      <SheetContent className="w-[420px] sm:w-[480px] overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="font-mono text-sm truncate">{span?.name}</SheetTitle>
        </SheetHeader>
        {span && (
          <div className="mt-4 flex flex-col gap-4 text-xs">
            <section>
              <p className="text-muted-foreground mb-2 uppercase tracking-wider text-[10px]">Span</p>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 font-mono">
                {[
                  ['trace_id', span.trace_id],
                  ['span_id', span.span_id],
                  ['parent', span.parent_span_id || '—'],
                  ['service', span.service_name],
                  ['kind', span.kind],
                  ['status', span.status_code === 2 ? 'ERROR' : 'OK'],
                  ['duration', fmtDuration(span.end_ns - span.start_ns)],
                ].map(([k, v]) => (
                  <>
                    <dt key={`k-${k}`} className="text-muted-foreground">{k}</dt>
                    <dd key={`v-${k}`} className="text-foreground break-all">{String(v)}</dd>
                  </>
                ))}
              </dl>
            </section>
            {Object.keys(attrs).length > 0 && (
              <section>
                <p className="text-muted-foreground mb-2 uppercase tracking-wider text-[10px]">Attributes</p>
                <pre className="bg-muted/30 rounded p-3 font-mono text-xs overflow-x-auto whitespace-pre-wrap break-all">
                  {JSON.stringify(attrs, null, 2)}
                </pre>
              </section>
            )}
            {Object.keys(resource).length > 0 && (
              <section>
                <p className="text-muted-foreground mb-2 uppercase tracking-wider text-[10px]">Resource</p>
                <pre className="bg-muted/30 rounded p-3 font-mono text-xs overflow-x-auto whitespace-pre-wrap break-all">
                  {JSON.stringify(resource, null, 2)}
                </pre>
              </section>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

function buildTree(spans: Span[]): Map<string, Span[]> {
  const children = new Map<string, Span[]>()
  for (const s of spans) {
    const pid = s.parent_span_id || ''
    if (!children.has(pid)) children.set(pid, [])
    children.get(pid)!.push(s)
  }
  return children
}

function flattenTree(children: Map<string, Span[]>, parentId = '', depth = 0): { span: Span; depth: number }[] {
  const result: { span: Span; depth: number }[] = []
  for (const s of children.get(parentId) ?? []) {
    result.push({ span: s, depth })
    result.push(...flattenTree(children, s.span_id, depth + 1))
  }
  return result
}

export default function TraceDetail() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const [spans, setSpans] = useState<Span[]>([])
  const [warnings, setWarnings] = useState<LintWarning[]>([])
  const [selected, setSelected] = useState<Span | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!traceId) return
    api.traces.get(traceId).then(r => {
      setSpans(r.data ?? [])
      setLoading(false)
    })
    api.lint.list().then(r => {
      setWarnings((r.data ?? []).filter(w => w.trace_id === traceId))
    })
  }, [traceId])

  const minNs = spans.reduce((m, s) => Math.min(m, s.start_ns), Infinity)
  const maxNs = spans.reduce((m, s) => Math.max(m, s.end_ns), 0)
  const totalNs = maxNs - minNs

  const children = buildTree(spans)
  const ordered = flattenTree(children)

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-3 px-6 py-4 border-b border-border">
        <button onClick={() => navigate(-1)} className="text-muted-foreground hover:text-foreground text-sm">←</button>
        <h1 className="font-mono text-sm text-foreground flex-1 truncate">{traceId}</h1>
        <StatusBadge code={spans[0]?.status_code ?? 0} />
        <span className="text-xs text-muted-foreground font-mono">{fmtDuration(totalNs)}</span>
      </div>

      <Tabs defaultValue="spans" className="flex-1 flex flex-col">
        <TabsList className="mx-6 mt-3 w-fit">
          <TabsTrigger value="spans">Spans ({spans.length})</TabsTrigger>
          <TabsTrigger value="lint">
            Lint {warnings.length > 0 && <Badge variant="destructive" className="ml-1 text-[10px] px-1">{warnings.length}</Badge>}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="spans" className="flex-1 overflow-hidden mt-0">
          {loading ? (
            <div className="p-8 text-muted-foreground text-sm">Loading…</div>
          ) : (
            <ScrollArea className="h-full">
              <div className="flex items-center gap-2 px-4 py-1 border-b border-border text-[10px] text-muted-foreground uppercase tracking-wider">
                <span className="w-48 flex-none">Span</span>
                <span className="flex-1">Timeline</span>
                <span className="w-20 text-right">Duration</span>
                <span className="w-20 text-right">Service</span>
              </div>
              {ordered.map(({ span, depth }) => (
                <SpanRow
                  key={span.span_id}
                  span={span}
                  minNs={minNs}
                  totalNs={totalNs}
                  depth={depth}
                  onClick={() => setSelected(span)}
                />
              ))}
            </ScrollArea>
          )}
        </TabsContent>

        <TabsContent value="lint" className="flex-1 overflow-hidden mt-0">
          <LintPanel warnings={warnings} onSelectSpan={spanId => {
            const s = spans.find(x => x.span_id === spanId)
            if (s) setSelected(s)
          }} />
        </TabsContent>
      </Tabs>

      <SpanAttributesPanel span={selected} open={!!selected} onClose={() => setSelected(null)} />
    </div>
  )
}
