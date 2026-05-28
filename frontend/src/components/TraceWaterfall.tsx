import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useVirtualizer } from '@tanstack/react-virtual'
import { AlertTriangle, X } from 'lucide-react'
import { Span, SpanEvent, LintWarning, TraceIssue, Log, api } from '@/lib/api'
import { SPAN_PALETTE as PALETTE, SPAN_ACCENT as ACCENT, svcColor, flatten, fmtNs, KIND_LABELS, buildTagMap, hasLinks, FlatSpan } from '@/lib/span-utils'
import { computeLayout, detectN1SpanIds, n1BannerEntries, n1IssueForSpan } from '@/components/trace-waterfall-utils'
import { fmtRelMs } from '@/lib/events-format'
import TraceGraph from '@/components/TraceGraph'

// ── layout constants ──────────────────────────────────────────────────────────

const NAME_W = 320
const ZERO_ID = '0000000000000000'
const DUR_W = 56
const ROW_H = 32
const STEPS = 6

// ── Ruler ─────────────────────────────────────────────────────────────────────

function Ruler({ traceDurNs, spanCount }: { traceDurNs: number; spanCount: number }) {
  return (
    <div
      className="grid items-center border-b border-border bg-muted shrink-0"
      style={{ gridTemplateColumns: `${NAME_W}px 1fr ${DUR_W}px` }}
    >
      <div className="px-3.5 py-[7px] font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
        span · {spanCount}
      </div>
      <div className="relative h-6 mx-2 border-l border-border">
        {Array.from({ length: STEPS + 1 }).map((_, i) => {
          // The rightmost tick (i===STEPS) sits at left: 100%, so its text
          // would overflow into the DUR header column to the right of the
          // timeline. Anchor that one tick's text to flow leftward instead.
          const isLast = i === STEPS
          return (
            <span
              key={i}
              className={[
                'absolute top-0 h-full whitespace-nowrap px-1 py-[7px] font-mono text-[9px] text-[var(--muted-foreground)]',
                i === 0 ? '' : 'border-l border-[var(--border)]',
                isLast ? 'text-right translate-x-[calc(-100%+1px)]' : '-translate-x-px',
              ].join(' ')}
              style={{ left: `${(i / STEPS) * 100}%` }}
            >
              {i === 0 ? '' : fmtNs((traceDurNs / STEPS) * i)}
            </span>
          )
        })}
      </div>
      <div className="pl-0 pr-2.5 py-[7px] text-right font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
        dur
      </div>
    </div>
  )
}

// ── MiniTimeline ──────────────────────────────────────────────────────────────

function MiniTimeline({ flatSpans, traceStartNs, traceDurNs, zoom }: {
  flatSpans: FlatSpan[]
  traceStartNs: number
  traceDurNs: number
  zoom: [number, number]
}) {
  const [zStart, zEnd] = zoom
  const zLeft  = ((zStart - traceStartNs) / traceDurNs) * 100
  const zWidth = ((zEnd - zStart) / traceDurNs) * 100

  return (
    <div className="relative h-[120px] px-4 py-2.5 bg-muted border-b border-border shrink-0">
      <div className="absolute left-4 right-4 top-3.5 bottom-2.5 bg-background rounded-[3px] overflow-hidden">
        {flatSpans.map(({ span, depth }) => {
          const c = svcColor(span.service_name)
          return (
            <div key={span.span_id} style={{
              position: 'absolute' as const,
              left: `${((span.start_ns - traceStartNs) / traceDurNs) * 100}%`,
              width: `${Math.max(0.3, (span.duration_ns / traceDurNs) * 100)}%`,
              top: depth * 9,
              height: 10,
              background: c.fg,
              opacity: 0.55,
            }} />
          )
        })}
        <div style={{
          position: 'absolute' as const,
          left: `${zLeft}%`,
          width: `${Math.max(1, zWidth)}%`,
          top: 0, bottom: 0,
          border: `1px solid ${ACCENT}`,
          borderRadius: 3,
          boxShadow: `0 0 0 3px ${ACCENT}30`,
          background: `${ACCENT}10`,
          pointerEvents: 'none' as const,
        }} />
      </div>
    </div>
  )
}

// ── SpanRow ───────────────────────────────────────────────────────────────────

function SpanRow({ flat, traceStartNs, traceDurNs, selected, hovered, tag, isN1, linked, onSelect, onHover }: {
  flat: FlatSpan
  traceStartNs: number
  traceDurNs: number
  selected: boolean
  hovered: boolean
  tag: string | undefined
  isN1: boolean
  linked: boolean
  onSelect: () => void
  onHover: (id: string | null) => void
}) {
  const { span, depth, orphan } = flat
  const c = svcColor(span.service_name)
  const isError = span.status_code === 2
  const tagColor = isN1 ? 'var(--warn)' : tag === 'n+1' ? '#ef4444' : '#f59e0b'

  const { leftPct: left, widthPct: width } = computeLayout(span, traceStartNs, traceDurNs)

  return (
    <div
      onClick={onSelect}
      onMouseEnter={() => onHover(span.span_id)}
      onMouseLeave={() => onHover(null)}
      className="grid items-center cursor-pointer border-b border-border transition-[background] duration-[80ms]"
      style={{
        gridTemplateColumns: `${NAME_W}px 1fr ${DUR_W}px`,
        height: ROW_H,
        background: selected
          ? `${ACCENT}1a`
          : hovered ? `${ACCENT}0d` : 'transparent',
        borderLeft: selected ? `2px solid ${ACCENT}` : '2px solid transparent',
      }}
      title={`${span.name}\n${span.service_name}\n${fmtNs(span.duration_ns)}`}
    >
      {/* name column */}
      <div
        className="flex items-center gap-[7px] min-w-0 overflow-hidden"
        style={{ paddingLeft: 14 + depth * 16 }}
      >
        {depth > 0 && (
          <span className="w-2 h-px bg-border shrink-0 -ml-[7px]" />
        )}
        {orphan && <AlertTriangle size={10} color="#f59e0b" className="shrink-0" />}
        <span
          className="w-[9px] h-[9px] rounded-sm opacity-85 shrink-0"
          style={{ background: c.fg }}
        />
        <span
          className="font-mono text-[10px] shrink-0 max-w-[90px] overflow-hidden text-ellipsis whitespace-nowrap opacity-75"
          style={{ color: c.fg }}
          title={span.service_name}
        >
          {span.service_name}
        </span>
        <span className="text-[10px] text-muted-foreground shrink-0 opacity-50">/</span>
        <span
          className="font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 min-w-0"
          style={{ color: isError ? '#ef4444' : 'var(--foreground)' }}
        >
          {span.name}
        </span>
        {isN1 && (
          <span className="font-mono text-[9px] font-bold text-warn-ink uppercase tracking-[0.08em] shrink-0 px-1 py-px rounded-[3px] bg-warn-bg">
            n+1
          </span>
        )}
        {!isN1 && tag && (
          <span
            className="font-mono text-[9px] font-bold uppercase tracking-[0.08em] shrink-0 px-1 py-px rounded-[3px]"
            style={{ color: tagColor, background: tagColor + '22' }}
          >
            {tag}
          </span>
        )}
        {linked && (
          <span
            data-testid="span-link-badge"
            title="span has links"
            className="font-mono text-[9px] shrink-0 text-[var(--accent)] opacity-80"
          >
            ⛓
          </span>
        )}
      </div>

      {/* timeline column */}
      <div className="relative h-[18px] mx-2">
        {/* dashed grid lines */}
        <div className="absolute inset-0 flex">
          {[0, 1, 2, 3].map(i => (
            <div
              key={i}
              className={`flex-1 ${i === 0 ? '' : 'border-l border-dashed border-border'}`}
            />
          ))}
        </div>
        {/* span bar */}
        <div style={{
          position: 'absolute',
          top: 4, height: 10,
          left: `${left}%`,
          width: `${width}%`,
          background: isError ? '#ef444418' : c.bg,
          borderRadius: 3,
          boxShadow: isError
            ? `inset 0 0 0 1px #ef444460, inset 2px 0 0 #ef4444`
            : `inset 0 0 0 1px ${c.fg}30, inset 2px 0 0 ${c.fg}`,
        }} />
        {/* warning outline for n+1 */}
        {(isN1 || tag === 'n+1') && (
          <div style={{
            position: 'absolute',
            top: 1, height: 16,
            left: `${left}%`,
            width: `${width}%`,
            borderRadius: 3,
            border: isN1 ? '1px solid var(--warn)' : '1px solid #ef4444',
            boxShadow: isN1 ? '0 0 0 2px var(--warn-bg)' : '0 0 0 2px #ef444430',
            pointerEvents: 'none',
          }} />
        )}
      </div>

      {/* duration column */}
      <div className="font-mono text-[10px] text-muted-foreground text-right pr-2.5">
        {fmtNs(span.duration_ns)}
      </div>
    </div>
  )
}

// ── FlameView ─────────────────────────────────────────────────────────────────

function FlameView({ flatSpans, traceStartNs, traceDurNs, tags, selectedId, hoveredId, onSelect, onHover, zoom, onZoom }: {
  flatSpans: FlatSpan[]
  traceStartNs: number
  traceDurNs: number
  tags: Map<string, string>
  selectedId: string | null
  hoveredId: string | null
  onSelect: (id: string) => void
  onHover: (id: string | null) => void
  zoom: [number, number]
  onZoom: (z: [number, number]) => void
}) {
  const [zStart, zEnd] = zoom
  const zDur = zEnd - zStart
  const zDurMs = zDur / 1_000_000

  const tickStepMs = zDurMs > 400 ? 100 : zDurMs > 200 ? 50 : zDurMs > 80 ? 20 : zDurMs > 20 ? 10 : 5
  const tickStepNs = tickStepMs * 1_000_000
  const tickStart = Math.ceil(zStart / tickStepNs) * tickStepNs
  const ticks: number[] = []
  for (let t = tickStart; t <= zEnd; t += tickStepNs) ticks.push(t)

  const maxDepth = flatSpans.reduce((m, f) => Math.max(m, f.depth), 0)
  const FLAME_ROW = 26
  const totalH = (maxDepth + 1) * FLAME_ROW

  const visible = flatSpans.filter(({ span }) =>
    span.start_ns + span.duration_ns >= zStart && span.start_ns <= zEnd,
  )

  const hovFlat = hoveredId ? flatSpans.find(f => f.span.span_id === hoveredId) : null

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* tick ruler */}
      <div className="relative h-6 border-b border-border bg-muted shrink-0">
        {ticks.map(t => (
          <span
            key={t}
            className="absolute top-0 bottom-0 border-l border-border font-mono text-[9px] text-muted-foreground py-[7px] px-1 whitespace-nowrap -translate-x-px"
            style={{ left: `${((t - zStart) / zDur) * 100}%` }}
          >
            +{fmtNs(t - traceStartNs)}
          </span>
        ))}
      </div>

      {/* flame canvas */}
      <div
        onMouseLeave={() => onHover(null)}
        className="relative flex-1 overflow-auto py-1.5"
        style={{ minHeight: totalH + 12 }}
      >
        {visible.map(({ span, depth }) => {
          const c = svcColor(span.service_name)
          const tag = tags.get(span.span_id)
          const isSel  = span.span_id === selectedId
          const isHov  = span.span_id === hoveredId
          const isError = span.status_code === 2
          const hot = tag === 'n+1'

          const clipStart = Math.max(span.start_ns, zStart)
          const clipEnd   = Math.min(span.end_ns, zEnd)
          const left  = ((clipStart - zStart) / zDur) * 100
          const width = Math.max(0.2, ((clipEnd - clipStart) / zDur) * 100)
          const top   = 6 + depth * FLAME_ROW

          return (
            <button
              key={span.span_id}
              type="button"
              onClick={e => {
                e.stopPropagation()
                onSelect(span.span_id)
                onZoom([span.start_ns, span.end_ns])
              }}
              onMouseEnter={() => onHover(span.span_id)}
              className="absolute rounded-[3px] px-[5px] font-mono text-[10px] font-semibold text-left cursor-pointer overflow-hidden text-ellipsis whitespace-nowrap transition-[background] duration-[80ms] outline-none"
              style={{
                left: `${left}%`, width: `${width}%`,
                top, height: FLAME_ROW - 4,
                background: isSel || isHov
                  ? `color-mix(in oklch, ${c.fg} 35%, ${c.bg})`
                  : isError ? '#ef444418' : c.bg,
                color: c.fg,
                border: hot ? '1px solid #ef4444' : isSel ? `1px solid ${c.fg}` : '1px solid transparent',
                boxShadow: isSel ? `inset 0 0 0 1px ${c.fg}` : 'none',
                transform: isHov ? 'translateY(-1px)' : 'none',
                zIndex: isHov || isSel ? 2 : 1,
              }}
              title={`${span.name} · ${fmtNs(span.duration_ns)}`}
            >
              {width > 5 ? (
                <>
                  <span className="opacity-[0.65]">{span.service_name.replace(/-service$/, '')}</span>
                  {' · '}
                  <span>{span.name}</span>
                </>
              ) : null}
            </button>
          )
        })}

        {/* hover tooltip */}
        {hovFlat && (() => {
          const { span, depth } = hovFlat
          const c   = svcColor(span.service_name)
          const tag = tags.get(span.span_id)
          const leftPct = ((span.start_ns - zStart) / zDur) * 100
          const top = 6 + depth * FLAME_ROW + (FLAME_ROW - 4) + 6
          return (
            <div
              className="absolute z-[5] bg-foreground text-background font-mono text-[11px] px-2.5 py-2 rounded-md min-w-[220px] max-w-[280px] pointer-events-none"
              style={{
                left: `clamp(8px, ${leftPct}%, calc(100% - 248px))`,
                top,
              }}
            >
              <div className="flex items-center gap-1.5 mb-1">
                <span className="w-1.5 h-1.5 rounded-full" style={{ background: c.fg }} />
                <span className="text-[9px] uppercase tracking-[0.14em] opacity-[0.65]">
                  {span.service_name}
                </span>
                {tag && (
                  <span className="ml-auto text-[9px] font-bold text-[#ff9b8a] uppercase">
                    {tag}
                  </span>
                )}
              </div>
              <div className="text-xs mb-1.5 overflow-hidden text-ellipsis whitespace-nowrap">
                {span.name}
              </div>
              <div className="flex gap-2.5 opacity-80">
                <span>dur <strong>{fmtNs(span.duration_ns)}</strong></span>
                <span>at <strong>+{fmtNs(span.start_ns - traceStartNs)}</strong></span>
              </div>
            </div>
          )
        })()}
      </div>

      {/* footer hint */}
      <div className="px-3.5 py-[5px] border-t border-border bg-muted font-mono text-[9.5px] text-muted-foreground flex gap-4 shrink-0">
        <span>click → zoom & select</span>
        <span>esc → reset zoom</span>
        <span className="flex-1" />
        <span>
          {fmtNs(zStart - traceStartNs)}–{fmtNs(zEnd - traceStartNs)} · {visible.length}/{flatSpans.length} spans
        </span>
      </div>
    </div>
  )
}

// ── Inspector helpers ─────────────────────────────────────────────────────────

function inspSevColor(n: number): string {
  if (n >= 17) return 'var(--danger)'
  if (n >= 13) return 'var(--warn)'
  if (n >= 9)  return 'var(--accent)'
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

function SpanLogs({ spanId }: { spanId: string }) {
  const [logs, setLogs]       = useState<Log[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setLogs([])
    api.logs.list({ spanId }).then(r => {
      setLogs(r.data ?? [])
      setLoading(false)
    }).catch(() => setLoading(false))
  }, [spanId])

  if (loading) {
    return (
      <div className="px-4 py-3.5 font-mono text-[11px] text-ink3">
        …
      </div>
    )
  }

  if (logs.length === 0) {
    return (
      <div className="px-4 py-3.5 font-mono text-[11px] text-ink3">
        no logs for this span
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      {logs.map((log, i) => (
        <div key={i} className="px-2.5 py-1 flex items-start gap-[7px] border-b border-[var(--line2)]">
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

function SpanEventsList({ spanId, spanStartNs }: { spanId: string; spanStartNs: number }) {
  const [events, setEvents] = useState<SpanEvent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setEvents([])
    api.spans.get(spanId).then(r => {
      setEvents(r.data?.events ?? [])
      setLoading(false)
    }).catch(() => setLoading(false))
  }, [spanId])

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
          <SpanEventRow key={i} event={e} spanStartNs={spanStartNs} last={i === events.length - 1} />
        ))}
      </div>
    </>
  )
}

function SpanEventRow({ event, spanStartNs, last }: { event: SpanEvent; spanStartNs: number; last: boolean }) {
  let attrs: Record<string, unknown> = {}
  try { attrs = JSON.parse(event.attributes || '{}') } catch { /* empty */ }

  const isException = event.name === 'exception'
  const excType  = typeof attrs['exception.type']       === 'string' ? attrs['exception.type']       as string : ''
  const excMsg   = typeof attrs['exception.message']    === 'string' ? attrs['exception.message']    as string : ''
  const excStack = typeof attrs['exception.stacktrace'] === 'string' ? attrs['exception.stacktrace'] as string : ''

  const inlineAttrs = Object.entries(attrs)
    .filter(([k]) => !k.startsWith('exception.'))
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(' ')

  return (
    <div className={`py-1 ${last ? '' : 'border-b border-[var(--line2)]'}`}>
      <div className="flex items-baseline gap-2.5">
        <span className={`w-[60px] shrink-0 font-mono text-[10px] ${isException ? 'text-[#ef4444]' : 'text-[var(--accent)]'}`}>
          {fmtRelMs(event.time_ns, spanStartNs)}
        </span>
        <span className={`min-w-0 flex-1 break-words font-mono text-[11px] ${isException ? 'text-[#ef4444]' : 'text-[var(--ink)]'}`}>
          {isException && (
            <span className="mr-1.5 inline-block size-1.5 rounded-full bg-[#ef4444] align-middle" />
          )}
          {event.name}
          {isException && excType && (
            <span className="text-[var(--ink2)]">{' · '}{excType}{excMsg ? `: ${excMsg}` : ''}</span>
          )}
        </span>
        {!isException && inlineAttrs && (
          <span className="font-mono text-[10px] text-[var(--ink2)]">{inlineAttrs}</span>
        )}
      </div>
      {isException && excStack && (
        <pre className="mx-0 mb-0.5 mt-1.5 max-h-[220px] overflow-auto whitespace-pre-wrap break-words rounded-[5px] border border-[color-mix(in_oklch,#ef4444_25%,var(--background))] bg-[color-mix(in_oklch,#ef4444_8%,var(--background))] px-2.5 py-1.5 font-mono text-[10px] text-[var(--ink)]">{excStack}</pre>
      )}
    </div>
  )
}

// ── SpanLinksList — outbound + incoming links section for the Inspector ─────

function SpanLinksList({ spanId, traceId }: { spanId: string; traceId: string }) {
  const [outbound, setOutbound] = useState<import('@/lib/api').SpanLink[]>([])
  const [incoming, setIncoming] = useState<import('@/lib/api').SpanLink[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancel = false
    setLoading(true)
    setOutbound([])
    setIncoming([])
    api.spans.get(spanId)
      .then(r => { if (!cancel) setOutbound(r.data?.links ?? []) })
      .catch(() => { /* leave empty */ })
      .finally(() => { if (!cancel) setLoading(false) })
    // Reverse lookup: who else links into this trace?
    fetch(`/api/traces/${encodeURIComponent(traceId)}/incoming-links`)
      .then(r => r.ok ? r.json() : { data: [] })
      .then(j => { if (!cancel) setIncoming((j?.data as import('@/lib/api').SpanLink[]) ?? []) })
      .catch(() => { /* leave empty */ })
    return () => { cancel = true }
  }, [spanId, traceId])

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

function SpanLinkRow({ link, last, incoming }: {
  link: import('@/lib/api').SpanLink
  last: boolean
  incoming?: boolean
}) {
  const targetTrace = incoming ? link.trace_id : link.linked_trace_id
  const targetSpan  = incoming ? link.span_id  : link.linked_span_id
  const short = targetTrace.length > 12
    ? `${targetTrace.slice(0, 4)}…${targetTrace.slice(-5)}`
    : targetTrace
  return (
    <Link
      to={`/traces/${encodeURIComponent(targetTrace)}`}
      className={`flex items-baseline gap-2 py-1 no-underline ${last ? '' : 'border-b border-[var(--line2)]'}`}
      data-testid="span-link-row"
    >
      <span className="font-mono text-[10px] text-[var(--accent)]">{incoming ? '←' : '→'}</span>
      <span className="font-mono text-[11px] text-[var(--foreground)]">{short}</span>
      <span className="font-mono text-[10px] text-[var(--muted-foreground)]">·</span>
      <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-[var(--muted-foreground)]" title={targetSpan}>
        span {targetSpan.length > 10 ? `${targetSpan.slice(0, 6)}…` : targetSpan}
      </span>
    </Link>
  )
}

// ── Inspector ─────────────────────────────────────────────────────────────────

const ISSUE_KIND_LABELS: Record<string, string> = {
  slow_db:         'slow DB query',
  chatty_http:     'chatty HTTP',
  large_payload:   'large payload',
  cache_miss_storm:'cache miss storm',
  tracing_gap:     'tracing gap',
  error_chain:     'error chain',
  serial_promise:  'serial promises',
  synchronous_io:  'synchronous I/O',
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
        {issue.count > 1 && <><strong>{issue.count}×</strong>{' '}</>}
        <code className="rounded-[3px] bg-[var(--surface)] px-1 font-mono">{issue.fingerprint}</code>
        {issue.wasted_ns > 0 && (
          <> — <strong>{fmtNs(issue.wasted_ns)}</strong> wasted</>
        )}
      </div>
    </div>
  )
}

function Inspector({ span, traceStartNs, warnings, issues, isN1, n1Count, onClose }: {
  span: Span
  traceStartNs: number
  warnings: LintWarning[]
  issues: TraceIssue[]
  isN1: boolean
  n1Count: number
  onClose: () => void
}) {
  const c = svcColor(span.service_name)
  const isError = span.status_code === 2
  const kind = KIND_LABELS[span.kind] ?? 'unknown'
  const spanWarnings = warnings.filter(w => w.span_id === span.span_id)

  const [activeTab, setActiveTab] = useState<'attrs' | 'logs'>('attrs')

  // Reset to attrs tab when span changes
  useEffect(() => { setActiveTab('attrs') }, [span.span_id])

  let attrs: Record<string, unknown> = {}
  let resource: Record<string, unknown> = {}
  try { attrs = JSON.parse(span.attributes) } catch { /* empty */ }
  try { resource = JSON.parse(span.resource) } catch { /* empty */ }

  const attrEntries = Object.entries(attrs)
  const resEntries  = Object.entries(resource).filter(([k]) => k !== 'service.name')

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
    <aside className="w-[320px] border-l border-border bg-background flex flex-col overflow-hidden shrink-0">
      {/* header */}
      <div className="px-4 py-3.5 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <span className="w-[9px] h-[9px] rounded-sm opacity-85" style={{ background: c.fg }} />
          <span className="font-mono text-[10px]" style={{ color: c.fg }}>
            {span.service_name}
          </span>
          <span className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.12em]">
            {kind}
          </span>
          <div className="flex-1" />
          <button
            onClick={onClose}
            className="bg-transparent border-none cursor-pointer text-muted-foreground p-0.5 rounded-[3px] flex items-center"
          >
            <X size={13} />
          </button>
        </div>
        <div
          className="font-mono text-xs leading-[1.4] break-words mb-3"
          style={{ color: isError ? '#ef4444' : 'var(--foreground)' }}
        >
          {span.name}
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
              +{fmtNs(span.start_ns - traceStartNs)}
            </div>
          </div>
        </div>
      </div>

      {/* Detector issue callouts — one card per matching TraceIssue on this span.
          N+1 keeps its specific messaging; all others get a generic card. */}
      {(() => {
        const spanIssues = issues.filter(
          i => i.example_span_id === span.span_id || i.parent_span_id === span.span_id
        )
        const n1Issue = n1IssueForSpan(span, issues)
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
                    <> <code className="rounded-[3px] bg-[var(--surface)] px-1 py-0 font-mono">{n1Issue.fingerprint}</code></>
                  )}
                  {n1Issue?.wasted_ns && n1Issue.wasted_ns > 0 ? (
                    <> totalling <strong>{fmtNs(n1Issue.wasted_ns)}</strong> of wasted time.</>
                  ) : '.'}
                  {' '}Batch with <code className="font-mono">WHERE … IN (?)</code>.
                </div>
              </div>
            )}
            {spanIssues.filter(i => i.kind !== 'n_plus_one').map(issue => (
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
              className="px-3 py-2.5 rounded-[7px] bg-[color-mix(in_oklch,#ef4444_10%,var(--background))] border border-[color-mix(in_oklch,#ef4444_30%,var(--background))]"
              style={{ marginBottom: i < spanWarnings.length - 1 ? 6 : 0 }}
            >
              <div className="flex items-center gap-1.5 mb-1">
                <span className="w-1.5 h-1.5 rounded-full bg-[#ef4444] shrink-0" />
                <span className="font-mono text-[9.5px] font-bold text-[#ef4444] tracking-[0.06em]">
                  {w.rule_id}
                </span>
              </div>
              <div className="text-[11px] text-foreground leading-[1.45]">
                {w.message}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* tab bar */}
      <div className="flex items-center border-b border-line px-3.5 font-mono text-[11px] shrink-0">
        <button type="button" onClick={() => setActiveTab('attrs')} style={tabStyle('attrs')}>attrs</button>
        <button type="button" onClick={() => setActiveTab('logs')}  style={tabStyle('logs')}>logs</button>
      </div>

      {/* attrs tab */}
      {activeTab === 'attrs' && (
        <div className="flex-1 overflow-auto flex flex-col">
          {attrEntries.length > 0 && (
            <>
              <div className="px-3.5 pt-3 pb-[5px] flex items-center font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
                attributes
                <span className="flex-1" />
                <span className="normal-case tracking-normal">{attrEntries.length}</span>
              </div>
              <AttrGrid entries={attrEntries} />
            </>
          )}
          {resEntries.length > 0 && (
            <>
              <div className="px-3.5 pt-3 pb-[5px] font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
                resource
              </div>
              <AttrGrid entries={resEntries} />
            </>
          )}
          <SpanEventsList spanId={span.span_id} spanStartNs={span.start_ns} />
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
        <SpanLogs spanId={span.span_id} />
      )}
    </aside>
  )
}

function AttrGrid({ entries }: { entries: [string, unknown][] }) {
  return (
    <div className="px-1.5 pb-1.5">
      {entries.map(([k, v]) => (
        <div
          key={k}
          className="grid gap-2 px-2 py-1 rounded"
          style={{ gridTemplateColumns: '1fr 1.4fr' }}
        >
          <div className="font-mono text-[10.5px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap">
            {k}
          </div>
          <div
            className="font-mono text-[10.5px] text-foreground overflow-hidden text-ellipsis whitespace-nowrap"
            title={String(v)}
          >
            {String(v)}
          </div>
        </div>
      ))}
    </div>
  )
}

// ── TraceMeta ─────────────────────────────────────────────────────────────────

function TraceMeta({ rootName, traceId, traceDurNs, spanCount, serviceCount, errorCount, lintCount, view, onView, zoom, traceStartNs, traceEndNs, onResetZoom }: {
  rootName: string
  traceId: string
  traceDurNs: number
  spanCount: number
  serviceCount: number
  errorCount: number
  lintCount: number
  view: 'waterfall' | 'flame' | 'graph'
  onView: (v: 'waterfall' | 'flame' | 'graph') => void
  zoom: [number, number]
  traceStartNs: number
  traceEndNs: number
  onResetZoom: () => void
}) {
  const isZoomed = zoom[0] > traceStartNs || zoom[1] < traceEndNs

  return (
    <div className="px-[18px] py-3 border-b border-border bg-background flex items-center gap-4 shrink-0">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <span className="font-mono text-[13px] font-semibold text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
            {rootName}
          </span>
          {errorCount > 0 && (
            <span className="font-mono text-[9px] font-bold text-[#ef4444] bg-[#ef444420] px-[5px] py-px rounded-[3px] uppercase">
              error
            </span>
          )}
          {lintCount > 0 && (
            <span className="font-mono text-[9px] text-[#f59e0b] bg-[#f59e0b20] px-[5px] py-px rounded-[3px] uppercase">
              {lintCount} lint
            </span>
          )}
        </div>
        <div className="font-mono text-[10px] text-muted-foreground">
          {traceId}
        </div>
      </div>

      {/* stats */}
      <StatBox label="total"  value={fmtNs(traceDurNs)} />
      <StatBox label="spans"  value={String(spanCount)} />
      <StatBox label="svcs"   value={String(serviceCount)} />

      {/* view toggle pill */}
      <div className="flex items-center bg-muted rounded-lg p-[3px] border border-border gap-0.5">
        {(['waterfall', 'flame', 'graph'] as const).map(v => {
          const active = view === v
          return (
            <button
              key={v}
              type="button"
              onClick={() => onView(v)}
              className={`inline-flex items-center gap-[5px] px-2.5 py-1 rounded-md font-sans text-xs font-medium cursor-pointer outline-none whitespace-nowrap border ${active ? 'bg-background text-foreground border-border' : 'bg-transparent text-muted-foreground border-transparent'}`}
            >
              {v === 'waterfall' ? <WaterfallIcon /> : v === 'flame' ? <FlameIcon /> : <GraphIcon />}
              {v.charAt(0).toUpperCase() + v.slice(1)}
            </button>
          )
        })}
      </div>

      {/* reset zoom */}
      {isZoomed && (
        <button
          type="button"
          onClick={onResetZoom}
          className="px-2.5 py-1 rounded-md font-mono text-[11px] font-semibold cursor-pointer outline-none whitespace-nowrap"
          style={{
            background: `${ACCENT}18`,
            border: `1px solid ${ACCENT}`,
            color: ACCENT,
          }}
        >
          ↺ reset zoom
        </button>
      )}
    </div>
  )
}

function StatBox({ label, value }: { label: string; value: string }) {
  return (
    <div className="shrink-0">
      <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </div>
      <div className="font-mono font-semibold text-sm text-foreground leading-[1.1] mt-0.5">
        {value}
      </div>
    </div>
  )
}

function WaterfallIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1"   y="1.5" width="9" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="2.5" y="4.5" width="6" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="4"   y="7.5" width="4" height="1.6" fill="currentColor" opacity="0.85" />
    </svg>
  )
}

function GraphIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <circle cx="6"   cy="2"  r="1.4" fill="currentColor" opacity="0.85" />
      <circle cx="2.5" cy="9"  r="1.4" fill="currentColor" opacity="0.85" />
      <circle cx="9.5" cy="9"  r="1.4" fill="currentColor" opacity="0.85" />
      <line x1="6" y1="3.4" x2="2.7" y2="7.8" stroke="currentColor" strokeWidth="1" opacity="0.6" />
      <line x1="6" y1="3.4" x2="9.3" y2="7.8" stroke="currentColor" strokeWidth="1" opacity="0.6" />
    </svg>
  )
}

function FlameIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1"   y="1"   width="10" height="2.6" fill="currentColor" opacity="0.45" />
      <rect x="1"   y="4.2" width="5"  height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="6.5" y="4.2" width="4"  height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="1.5" y="7.4" width="3"  height="2.6" fill="currentColor" opacity="0.9" />
    </svg>
  )
}

// ── N+1 Issue Banner ──────────────────────────────────────────────────────────

function N1Banner({ issues }: { issues: { fingerprint: string; count: number; wastedNs: number }[] }) {
  if (issues.length === 0) return null
  const top = issues[0]
  const fp = top.fingerprint.length > 60 ? top.fingerprint.slice(0, 57) + '…' : top.fingerprint
  return (
    <div className="px-4 py-[9px] border-b border-border bg-warn-bg border-l-[3px] border-l-warn flex items-center gap-2.5 shrink-0">
      <AlertTriangle size={13} color="var(--warn)" className="shrink-0" />
      <span className="font-mono text-[11px] text-warn-ink flex-1 min-w-0">
        <strong>N+1 detected</strong>
        {' — '}
        <span className="opacity-85">{fp}</span>
        {' called '}
        <strong>{top.count}×</strong>
        {' — '}
        <strong>{fmtNs(top.wastedNs)}</strong>
        {' wasted'}
        {issues.length > 1 && (
          <span className="ml-2 opacity-70">+{issues.length - 1} more</span>
        )}
      </span>
    </div>
  )
}

// ── TraceWaterfall (main export) ──────────────────────────────────────────────

interface Props {
  spans: Span[]
  warnings?: LintWarning[]
  issues?: TraceIssue[]
  traceId?: string
}

export default function TraceWaterfall({ spans, warnings = [], issues = [], traceId = '' }: Props) {
  const traceStartNs = spans.reduce((m, s) => Math.min(m, s.start_ns), Infinity)
  const traceEndNs   = spans.reduce((m, s) => Math.max(m, s.end_ns), 0)
  const traceDurNs   = traceEndNs - traceStartNs

  // Deep-link: ?span=X (or legacy ?spanId=X) pre-selects a span; ?view= seeds the view.
  const urlParams = typeof window !== 'undefined'
    ? new URLSearchParams(window.location.search) : new URLSearchParams()
  const initialSelected = urlParams.get('span') ?? urlParams.get('spanId')
  const initialView = (['waterfall', 'flame', 'graph'] as const)
    .find(v => v === urlParams.get('view')) ?? 'waterfall'

  const [view,       setView]       = useState<'waterfall' | 'flame' | 'graph'>(initialView)
  const [selectedId, setSelectedId] = useState<string | null>(initialSelected)
  const [hoveredId,  setHoveredId]  = useState<string | null>(null)
  const [zoom,       setZoom]       = useState<[number, number]>([traceStartNs, traceEndNs])

  useEffect(() => {
    if (Number.isFinite(traceStartNs)) setZoom([traceStartNs, traceEndNs])
  }, [traceStartNs, traceEndNs])

  const resetZoom = useCallback(() => {
    setZoom([traceStartNs, traceEndNs])
  }, [traceStartNs, traceEndNs])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') resetZoom() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [resetZoom])

  const flatSpans    = flatten(spans)
  const tags         = buildTagMap(warnings)

  const n1SpanIds = useMemo(() => detectN1SpanIds(flatSpans, issues), [flatSpans, issues])
  const n1Banner  = useMemo(() => n1BannerEntries(flatSpans, issues), [flatSpans, issues])

  const rootSpan     = spans.find(s => !s.parent_span_id || s.parent_span_id === ZERO_ID)
  const serviceCount = new Set(spans.map(s => s.service_name)).size
  const errorCount   = spans.filter(s => s.status_code === 2).length
  const selectedSpan = selectedId ? (spans.find(s => s.span_id === selectedId) ?? null) : null

  const handleSelect = useCallback((spanId: string) => {
    setSelectedId(prev => prev === spanId ? null : spanId)
  }, [])

  // Keep ?span= and ?view= in sync so the back button is not polluted.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (selectedId) { params.set('span', selectedId) } else { params.delete('span') }
    params.set('view', view)
    const newSearch = params.toString()
    if (newSearch !== window.location.search.replace(/^\?/, '')) {
      history.replaceState(null, '', `${window.location.pathname}?${newSearch}`)
    }
  }, [selectedId, view])

  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: flatSpans.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_H,
    overscan: 10,
  })

  // After mount, scroll the deep-linked span into view (?spanId=X).
  useEffect(() => {
    if (!initialSelected || flatSpans.length === 0) return
    const idx = flatSpans.findIndex(f => f.span.span_id === initialSelected)
    if (idx >= 0) virtualizer.scrollToIndex(idx, { align: 'center' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialSelected, flatSpans.length])

  if (spans.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground font-mono text-[13px]">
        No spans
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <TraceMeta
        rootName={rootSpan?.name ?? traceId}
        traceId={traceId}
        traceDurNs={traceDurNs}
        spanCount={spans.length}
        serviceCount={serviceCount}
        errorCount={errorCount}
        lintCount={warnings.length}
        view={view}
        onView={setView}
        zoom={zoom}
        traceStartNs={traceStartNs}
        traceEndNs={traceEndNs}
        onResetZoom={resetZoom}
      />
      <N1Banner issues={n1Banner} />
      <MiniTimeline
        flatSpans={flatSpans}
        traceStartNs={traceStartNs}
        traceDurNs={traceDurNs}
        zoom={zoom}
      />

      <div className="flex-1 flex overflow-hidden">
        {/* left: waterfall or flame */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {view === 'waterfall' ? (
            <>
              <Ruler traceDurNs={traceDurNs} spanCount={spans.length} />
              <div ref={parentRef} className="flex-1 overflow-y-auto overflow-x-hidden">
                <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
                  {virtualizer.getVirtualItems().map(vRow => {
                    const flat = flatSpans[vRow.index]
                    return (
                      <div
                        key={flat.span.span_id}
                        className="absolute left-0 right-0"
                        style={{ top: vRow.start, height: ROW_H }}
                      >
                        <SpanRow
                          flat={flat}
                          traceStartNs={traceStartNs}
                          traceDurNs={traceDurNs}
                          selected={flat.span.span_id === selectedId}
                          hovered={flat.span.span_id === hoveredId}
                          tag={tags.get(flat.span.span_id)}
                          isN1={n1SpanIds.has(flat.span.span_id)}
                          linked={hasLinks(flat.span)}
                          onSelect={() => handleSelect(flat.span.span_id)}
                          onHover={setHoveredId}
                        />
                      </div>
                    )
                  })}
                </div>
              </div>
            </>
          ) : view === 'flame' ? (
            <FlameView
              flatSpans={flatSpans}
              traceStartNs={traceStartNs}
              traceDurNs={traceDurNs}
              tags={tags}
              selectedId={selectedId}
              hoveredId={hoveredId}
              onSelect={handleSelect}
              onHover={setHoveredId}
              zoom={zoom}
              onZoom={setZoom}
            />
          ) : (
            <TraceGraph
              spans={spans}
              selectedId={selectedId}
              onSelect={handleSelect}
            />
          )}
        </div>

        {/* right: inspector */}
        {selectedSpan && (() => {
          const selIsN1 = n1SpanIds.has(selectedSpan.span_id)
          // Compute count for the selected span's db.statement
          let selN1Count = 0
          if (selIsN1) {
            let attrs: Record<string, unknown> = {}
            try { attrs = JSON.parse(selectedSpan.attributes ?? '{}') } catch { /* empty */ }
            const stmt = attrs['db.statement']
            if (typeof stmt === 'string') {
              for (const flat of flatSpans) {
                let a: Record<string, unknown> = {}
                try { a = JSON.parse(flat.span.attributes ?? '{}') } catch { /* empty */ }
                if (a['db.statement'] === stmt) selN1Count++
              }
            }
          }
          return (
            <Inspector
              span={selectedSpan}
              traceStartNs={traceStartNs}
              warnings={warnings}
              issues={issues}
              isN1={selIsN1}
              n1Count={selN1Count}
              onClose={() => setSelectedId(null)}
            />
          )
        })()}
      </div>
    </div>
  )
}
