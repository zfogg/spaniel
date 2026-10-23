import { useQuery } from '@tanstack/react-query'
import { qk } from '@/lib/query'
import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'
import { Span, SpanRow, SpanEvent, LintWarning, TraceIssue, api } from '@/lib/api'
import { fmtNs, KIND_LABELS, httpDisplayName } from '@/lib/span-utils'
import { n1IssueForSpan } from '@/components/trace-waterfall-utils'
import { fmtRelMs } from '@/lib/events-format'
import JsonView from '@/components/JsonView'
import ServiceNameBadge from '@/components/ServiceNameBadge'
import { TimestampWithAgo } from '@/components/shared/TimestampWithAgo'

// ── Inspector helpers ─────────────────────────────────────────────────────────

function inspSevColor(n: number): string {
  if (n >= 17) return 'var(--danger)'
  if (n >= 13) return 'var(--warn)'
  if (n >= 9) return 'var(--accent)'
  return 'var(--ink3)'
}

function inspBodyColor(n: number): string {
  if (n >= 13) return 'var(--ink)'
  return 'var(--ink2)'
}

function inspFmtRelative(ns: number): string {
  const diffMs = Date.now() - ns / 1_000_000
  if (diffMs < 0) return 'just now'
  if (diffMs < 1000) return `${Math.round(diffMs)}ms ago`
  if (diffMs < 60_000) return `${Math.round(diffMs / 1000)}s ago`
  return `${Math.round(diffMs / 60_000)}m ago`
}

// ── SpanLogs ──────────────────────────────────────────────────────────────────

function SpanLogs({
  spanId,
  traceId,
  sessionId,
}: {
  spanId: string
  traceId: string
  sessionId?: string
}) {
  const params = { spanId, traceId, sessionId }
  const { data: logs = [], isPending: loading } = useQuery({
    queryKey: qk.logs(params),
    queryFn: () => api.logs.list(params),
    select: (response) => response.data ?? [],
  })

  if (loading) {
    return <div className="px-4 py-3.5 font-mono text-[11px] text-ink3">…</div>
  }

  if (logs.length === 0) {
    return <div className="px-4 py-3.5 font-mono text-[11px] text-ink3">no logs for this span</div>
  }

  return (
    <div className="flex-1 overflow-auto">
      {logs.map((log, i) => (
        <div
          key={i}
          className="px-2.5 py-1 flex items-start gap-[7px] border-b border-[var(--line2)]"
        >
          {/* severity dot */}
          <span
            className="w-[5px] h-[5px] rounded-full shrink-0 mt-1"
            style={{ background: inspSevColor(log.severity) }}
          />
          {/* timestamp */}
          <span className="font-mono text-[10px] text-ink3 shrink-0 whitespace-nowrap">
            {inspFmtRelative(log.timestamp_ns)}
          </span>
          {/* body */}
          <span
            className="font-mono text-[10.5px] leading-[1.4] break-words min-w-0"
            style={{ color: inspBodyColor(log.severity) }}
          >
            {log.body}
          </span>
        </div>
      ))}
    </div>
  )
}

// ── SpanEventsList — events section for the Inspector ────────────────────────

function useInspectorSpan(spanId: string, traceId: string) {
  return useQuery({
    queryKey: ['trace-span', { spanId, traceId }],
    queryFn: () => api.spans.get(spanId).then((response) => response.data),
  })
}

function SpanEventsList({
  spanId,
  traceId,
  spanStartNs,
}: {
  spanId: string
  traceId: string
  spanStartNs: number
}) {
  const { data, isPending: loading } = useInspectorSpan(spanId, traceId)
  const events: SpanEvent[] = data?.events ?? []

  if (loading) {
    return <div className="px-3.5 py-1.5 font-mono text-[10px] text-[var(--ink3)]">…</div>
  }
  if (events.length === 0) return null

  return (
    <>
      <div className="flex items-center px-3.5 pt-3 pb-1 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--muted-foreground)]">
        <span>events</span>
        <span className="flex-1" />
        <span className="normal-case tracking-normal">{events.length}</span>
      </div>
      <div className="px-3.5 pb-3" data-testid="span-events">
        {events.map((e, i) => (
          <SpanEventRow
            key={i}
            event={e}
            spanStartNs={spanStartNs}
            last={i === events.length - 1}
          />
        ))}
      </div>
    </>
  )
}

function SpanEventRow({
  event,
  spanStartNs,
  last,
}: {
  event: SpanEvent
  spanStartNs: number
  last: boolean
}) {
  let attrs: Record<string, unknown> = {}
  try {
    attrs = JSON.parse(event.attributes || '{}')
  } catch {
    /* empty */
  }

  const isException = event.name === 'exception'
  const excType =
    typeof attrs['exception.type'] === 'string' ? (attrs['exception.type'] as string) : ''
  const excMsg =
    typeof attrs['exception.message'] === 'string' ? (attrs['exception.message'] as string) : ''
  const excStack =
    typeof attrs['exception.stacktrace'] === 'string'
      ? (attrs['exception.stacktrace'] as string)
      : ''

  const inlineAttrs = Object.entries(attrs)
    .filter(([k]) => !k.startsWith('exception.'))
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(' ')

  return (
    <div className={`py-1 ${last ? '' : 'border-b border-[var(--line2)]'}`}>
      <div className="flex items-baseline gap-2.5">
        <span
          className={`w-[60px] shrink-0 font-mono text-[10px] ${isException ? 'text-danger' : 'text-[var(--accent)]'}`}
        >
          {fmtRelMs(event.time_ns, spanStartNs)}
        </span>
        <span
          className={`min-w-0 flex-1 break-words font-mono text-[11px] ${isException ? 'text-danger' : 'text-[var(--ink)]'}`}
        >
          {isException && (
            <span className="mr-1.5 inline-block size-1.5 rounded-full bg-danger align-middle" />
          )}
          {event.name}
          {isException && excType && (
            <span className="text-[var(--ink2)]">
              {' · '}
              {excType}
              {excMsg ? `: ${excMsg}` : ''}
            </span>
          )}
        </span>
        {!isException && inlineAttrs && (
          <span className="font-mono text-[10px] text-[var(--ink2)]">{inlineAttrs}</span>
        )}
      </div>
      {isException && excStack && (
        <pre className="mx-0 mb-0.5 mt-1.5 max-h-[220px] overflow-auto whitespace-pre-wrap break-words rounded-[5px] border border-[color-mix(in_oklch,var(--danger)_25%,var(--background))] bg-[color-mix(in_oklch,var(--danger)_8%,var(--background))] px-2.5 py-1.5 font-mono text-[10px] text-[var(--ink)]">
          {excStack}
        </pre>
      )}
    </div>
  )
}

// ── SpanLinksList — outbound + incoming links section for the Inspector ─────

function SpanLinksList({ spanId, traceId }: { spanId: string; traceId: string }) {
  const span = useInspectorSpan(spanId, traceId)
  const links = useQuery({
    queryKey: ['trace-incoming-links', traceId],
    queryFn: async () => {
      const response = await fetch(`/api/traces/${encodeURIComponent(traceId)}/incoming-links`)
      if (!response.ok) throw new Error('Unable to load incoming links')
      const result = await response.json()
      return (result.data ?? []) as import('@/lib/api').SpanLink[]
    },
  })
  const outbound = span.data?.links ?? []
  const incoming = links.data ?? []
  const loading = span.isPending || links.isPending

  if (loading) {
    return <div className="px-3.5 py-1.5 font-mono text-[10px] text-[var(--ink3)]">…</div>
  }
  if (outbound.length === 0 && incoming.length === 0) return null

  return (
    <div data-testid="span-links">
      {outbound.length > 0 && (
        <>
          <div className="flex items-center px-3.5 pt-3 pb-1 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--muted-foreground)]">
            <span>links</span>
            <span className="flex-1" />
            <span className="normal-case tracking-normal">{outbound.length}</span>
          </div>
          <div className="px-3.5 pb-2">
            {outbound.map((l, i) => (
              <SpanLinkRow key={`out-${i}`} link={l} last={i === outbound.length - 1} />
            ))}
          </div>
        </>
      )}
      {incoming.length > 0 && (
        <>
          <div className="flex items-center px-3.5 pt-3 pb-1 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--muted-foreground)]">
            <span>incoming links</span>
            <span className="flex-1" />
            <span className="normal-case tracking-normal">{incoming.length}</span>
          </div>
          <div className="px-3.5 pb-3">
            {incoming.map((l, i) => (
              <SpanLinkRow key={`in-${i}`} link={l} incoming last={i === incoming.length - 1} />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function SpanLinkRow({
  link,
  last,
  incoming,
}: {
  link: import('@/lib/api').SpanLink
  last: boolean
  incoming?: boolean
}) {
  const targetTrace = incoming ? link.trace_id : link.linked_trace_id
  const targetSpan = incoming ? link.span_id : link.linked_span_id
  const short =
    targetTrace.length > 12 ? `${targetTrace.slice(0, 4)}…${targetTrace.slice(-5)}` : targetTrace
  return (
    <Link
      to={`/traces/${encodeURIComponent(targetTrace)}`}
      className={`flex items-baseline gap-2 py-1 no-underline ${last ? '' : 'border-b border-[var(--line2)]'}`}
      data-testid="span-link-row"
    >
      <span className="font-mono text-[10px] text-[var(--accent)]">{incoming ? '←' : '→'}</span>
      <span className="font-mono text-[11px] text-[var(--foreground)]">{short}</span>
      <span className="font-mono text-[10px] text-[var(--muted-foreground)]">·</span>
      <span
        className="min-w-0 flex-1 truncate font-mono text-[10px] text-[var(--muted-foreground)]"
        title={targetSpan}
      >
        span {targetSpan.length > 10 ? `${targetSpan.slice(0, 6)}…` : targetSpan}
      </span>
    </Link>
  )
}

// ── Inspector ─────────────────────────────────────────────────────────────────

const ISSUE_KIND_LABELS: Record<string, string> = {
  slow_db: 'slow DB query',
  chatty_http: 'chatty HTTP',
  large_payload: 'large payload',
  cache_miss_storm: 'cache miss storm',
  tracing_gap: 'tracing gap',
  error_chain: 'error chain',
  serial_promise: 'serial promises',
  synchronous_io: 'synchronous I/O',
}

function IssueCallout({ issue }: { issue: import('@/lib/api').TraceIssue }) {
  const label = ISSUE_KIND_LABELS[issue.kind] ?? issue.kind.replace(/_/g, ' ')
  return (
    <div
      data-testid="issue-callout"
      data-kind={issue.kind}
      className="mx-3.5 mt-3 rounded-lg border px-3 py-2.5"
      style={{
        background: 'color-mix(in oklch, var(--warn) 14%, var(--surface))',
        borderColor: 'color-mix(in oklch, var(--warn) 35%, var(--surface))',
      }}
    >
      <div className="mb-1 flex items-center gap-1.5">
        <span className="size-1.5 shrink-0 rounded-full bg-[var(--warn)]" />
        <span className="font-mono text-[10px] font-bold tracking-[0.06em] text-[var(--warn-ink)] uppercase">
          {label}
        </span>
      </div>
      <div className="text-[11.5px] leading-snug text-[var(--ink)]">
        {issue.count > 1 && (
          <>
            <strong>{issue.count}×</strong>{' '}
          </>
        )}
        <code className="rounded-[3px] bg-[var(--surface)] px-1 font-mono">
          {issue.fingerprint}
        </code>
        {issue.wasted_ns > 0 && (
          <>
            {' '}
            — <strong>{fmtNs(issue.wasted_ns)}</strong> wasted
          </>
        )}
      </div>
    </div>
  )
}

export default function SpanInspector({
  span,
  spans = [],
  traceStartNs,
  warnings = [],
  issues = [],
  isN1 = false,
  n1Count = 0,
  onClose,
}: {
  span: Span | SpanRow
  spans?: Span[]
  traceStartNs?: number
  warnings?: LintWarning[]
  issues?: TraceIssue[]
  isN1?: boolean
  n1Count?: number
  onClose: () => void
}) {
  const isError = span.status_code === 2
  const kind = KIND_LABELS[span.kind] ?? 'unknown'
  const spanWarnings = warnings.filter((w) => w.span_id === span.span_id)

  const [activeTab, setActiveTab] = useState<'attrs' | 'logs'>('attrs')

  // Reset to attrs tab when span changes
  useEffect(() => {
    setActiveTab('attrs')
  }, [span.span_id])

  let attrs: Record<string, unknown> = {}
  let resource: Record<string, unknown> = {}
  try {
    attrs = JSON.parse(span.attributes)
  } catch {
    /* empty */
  }
  try {
    resource = JSON.parse(span.resource)
  } catch {
    /* empty */
  }

  const attrEntries = Object.entries(attrs)
  const resEntries = Object.entries(resource).filter(([k]) => k !== 'service.name')

  function tabStyle(tab: 'attrs' | 'logs'): React.CSSProperties {
    const active = activeTab === tab
    return {
      padding: '6px 10px',
      fontFamily: 'var(--font-mono)',
      fontSize: 11,
      fontWeight: active ? 600 : 400,
      color: active ? 'var(--ink)' : 'var(--ink3)',
      borderBottom: active ? '2px solid var(--accent)' : '2px solid transparent',
      background: 'none',
      border: 'none',
      borderBottomStyle: 'solid',
      borderBottomWidth: 2,
      borderBottomColor: active ? 'var(--accent)' : 'transparent',
      cursor: 'pointer',
      outline: 'none',
      marginBottom: -1,
    }
  }

  return (
    <aside
      data-testid="span-inspector"
      aria-label="Span inspector"
      tabIndex={0}
      className="sticky right-0 z-10 h-full w-[512px] border-l border-border bg-background min-h-0 overflow-y-auto overscroll-contain shrink-0"
    >
      {/* header */}
      <div className="px-4 py-3.5 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <ServiceNameBadge name={span.service_name} />
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.12em]">
            {kind}
          </span>
          <div className="flex-1" />
          <button
            type="button"
            aria-label="Close inspector"
            onClick={onClose}
            className="bg-transparent border-none cursor-pointer text-muted-foreground p-0.5 rounded-[3px] flex items-center"
          >
            <X size={13} />
          </button>
        </div>
        <div
          className="font-mono text-xs leading-[1.4] break-words mb-3"
          style={{ color: isError ? 'var(--danger)' : 'var(--foreground)' }}
        >
          {httpDisplayName(span)}
        </div>
        <div className="flex gap-4 items-baseline">
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-0.5">
              duration
            </div>
            <div className="font-mono text-[19px] font-semibold text-foreground">
              {fmtNs(span.duration_ns)}
            </div>
          </div>
          <div>
            <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-0.5">
              started
            </div>
            <div className="font-mono text-xs text-foreground">
              {traceStartNs === undefined ? (
                <TimestampWithAgo nanoseconds={span.start_ns} stacked />
              ) : (
                <>+{fmtNs(span.start_ns - traceStartNs)}</>
              )}
            </div>
          </div>
        </div>
      </div>

      <div className="px-4 py-3 border-b border-border bg-muted font-mono text-[10px]">
        <div className="text-muted-foreground mb-1">belongs to trace</div>
        <div className="break-all">{span.trace_id}</div>
        <Link
          className="text-accent underline decoration-dotted"
          to={`/traces/${span.trace_id}?view=waterfall&span=${span.span_id}`}
        >
          open waterfall
        </Link>
      </div>

      {/* Detector issue callouts — one card per matching TraceIssue on this span.
          N+1 keeps its specific messaging; all others get a generic card. */}
      {(() => {
        const spanIssues = issues.filter(
          (i) => i.example_span_id === span.span_id || i.parent_span_id === span.span_id,
        )
        const n1Issue = n1IssueForSpan(span, issues, spans)
        // Include legacy isN1 signal even without a persisted issue.
        const showN1 = n1Issue || isN1

        return (
          <>
            {showN1 && (
              <div
                data-testid="n1-callout"
                className="mx-3.5 mt-3 rounded-lg border px-3 py-2.5"
                style={{
                  background: 'color-mix(in oklch, var(--danger) 18%, var(--surface))',
                  borderColor: 'color-mix(in oklch, var(--danger) 40%, var(--surface))',
                }}
              >
                <div className="mb-1 flex items-center gap-1.5">
                  <span className="size-1.5 shrink-0 rounded-full bg-[var(--danger)]" />
                  <span className="font-mono text-[10px] font-bold tracking-[0.06em] text-[var(--danger-ink)]">
                    N+1 SUSPECTED
                  </span>
                </div>
                <div className="text-[11.5px] leading-snug text-[var(--ink)]">
                  <strong>{n1Issue?.count ?? n1Count}</strong> sibling spans share fingerprint
                  {n1Issue?.fingerprint && (
                    <>
                      {' '}
                      <code className="rounded-[3px] bg-[var(--surface)] px-1 py-0 font-mono">
                        {n1Issue.fingerprint}
                      </code>
                    </>
                  )}
                  {n1Issue?.wasted_ns && n1Issue.wasted_ns > 0 ? (
                    <>
                      {' '}
                      totalling <strong>{fmtNs(n1Issue.wasted_ns)}</strong> of wasted time.
                    </>
                  ) : (
                    '.'
                  )}{' '}
                  Batch with <code className="font-mono">WHERE … IN (?)</code>.
                </div>
              </div>
            )}
            {spanIssues
              .filter((i) => i.kind !== 'n_plus_one')
              .map((issue) => (
                <IssueCallout key={issue.id} issue={issue} />
              ))}
          </>
        )
      })()}

      {/* warning callouts */}
      {spanWarnings.length > 0 && (
        <div className="mx-3.5 mt-3">
          {spanWarnings.map((w, i) => (
            <div
              key={i}
              className="px-3 py-2.5 rounded-[7px] bg-[color-mix(in_oklch,var(--danger)_10%,var(--background))] border border-[color-mix(in_oklch,var(--danger)_30%,var(--background))]"
              style={{ marginBottom: i < spanWarnings.length - 1 ? 6 : 0 }}
            >
              <div className="flex items-center gap-1.5 mb-1">
                <span className="w-1.5 h-1.5 rounded-full bg-danger shrink-0" />
                <span className="font-mono text-[9.5px] font-bold text-danger tracking-[0.06em]">
                  {w.rule_id}
                </span>
              </div>
              <div className="text-[11px] text-foreground leading-[1.45]">{w.message}</div>
            </div>
          ))}
        </div>
      )}

      {/* tab bar */}
      <div className="flex items-center border-b border-line px-3.5 font-mono text-[11px] shrink-0">
        <button type="button" onClick={() => setActiveTab('attrs')} style={tabStyle('attrs')}>
          attrs
        </button>
        <button type="button" onClick={() => setActiveTab('logs')} style={tabStyle('logs')}>
          logs
        </button>
      </div>

      {/* attrs tab */}
      {activeTab === 'attrs' && (
        <div className="flex flex-col">
          {attrEntries.length > 0 && (
            <>
              <div className="px-3.5 pt-3 pb-[5px] flex items-center font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
                attributes
                <span className="flex-1" />
                <span className="normal-case tracking-normal">{attrEntries.length}</span>
              </div>
              <JsonView data={attrs} />
            </>
          )}
          {resEntries.length > 0 && (
            <>
              <div className="px-3.5 pt-3 pb-[5px] font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
                resource
              </div>
              <JsonView data={Object.fromEntries(resEntries)} />
            </>
          )}
          <SpanEventsList
            spanId={span.span_id}
            traceId={span.trace_id}
            spanStartNs={span.start_ns}
          />
          <SpanLinksList spanId={span.span_id} traceId={span.trace_id} />
          {attrEntries.length === 0 && resEntries.length === 0 && (
            <div className="px-4 py-[18px] font-mono text-[11px] text-muted-foreground">
              no attributes
            </div>
          )}
        </div>
      )}

      {/* logs tab */}
      {activeTab === 'logs' && (
        <SpanLogs spanId={span.span_id} traceId={span.trace_id} sessionId={span.session_id} />
      )}
    </aside>
  )
}
