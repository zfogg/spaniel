import LogLevelBadge from '@/components/LogLevelBadge'
import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { AlertTriangle } from 'lucide-react'
import { Span, LintWarning, TraceIssue, Log } from '@/lib/api'
import {
  SPAN_ACCENT as ACCENT,
  svcColor,
  flatten,
  fmtNs,
  buildTagMap,
  hasLinks,
  FlatSpan,
  httpDisplayName,
} from '@/lib/span-utils'
import { computeLayout, detectN1SpanIds, n1BannerEntries } from '@/components/trace-waterfall-utils'
import TraceGraph from '@/components/TraceGraph'
import SpanInspector from '@/components/SpanInspector'

// ── layout constants ──────────────────────────────────────────────────────────

const NAME_W = 720
const ZERO_ID = '0000000000000000'
const DUR_W = 56
const TIMELINE_MIN_W = 420
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

function MiniTimeline({
  flatSpans,
  traceStartNs,
  traceDurNs,
  zoom,
}: {
  flatSpans: FlatSpan[]
  traceStartNs: number
  traceDurNs: number
  zoom: [number, number]
}) {
  const [zStart, zEnd] = zoom
  const zLeft = ((zStart - traceStartNs) / traceDurNs) * 100
  const zWidth = ((zEnd - zStart) / traceDurNs) * 100

  return (
    <div className="relative h-[120px] px-4 py-2.5 bg-muted border-b border-border shrink-0">
      <div className="absolute left-4 right-4 top-3.5 bottom-2.5 bg-background rounded-[3px] overflow-hidden">
        {flatSpans.map(({ span, depth }) => {
          const c = svcColor(span.service_name)
          return (
            <div
              key={span.span_id}
              style={{
                position: 'absolute' as const,
                left: `${((span.start_ns - traceStartNs) / traceDurNs) * 100}%`,
                width: `${Math.max(0.3, (span.duration_ns / traceDurNs) * 100)}%`,
                top: depth * 9,
                height: 10,
                background: c.fg,
                opacity: 0.55,
              }}
            />
          )
        })}
        <div
          style={{
            position: 'absolute' as const,
            left: `${zLeft}%`,
            width: `${Math.max(1, zWidth)}%`,
            top: 0,
            bottom: 0,
            border: `1px solid ${ACCENT}`,
            borderRadius: 3,
            boxShadow: `0 0 0 3px ${ACCENT}30`,
            background: `${ACCENT}10`,
            pointerEvents: 'none' as const,
          }}
        />
      </div>
    </div>
  )
}

// ── SpanRow ───────────────────────────────────────────────────────────────────

function SpanRow({
  flat,
  traceStartNs,
  traceDurNs,
  selected,
  hovered,
  tag,
  isN1,
  linked,
  onSelect,
  onHover,
}: {
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
  const tagColor = isN1 ? 'var(--warn)' : tag === 'n+1' ? 'var(--danger)' : 'var(--warn)'

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
        background: selected ? `${ACCENT}1a` : hovered ? `${ACCENT}0d` : 'transparent',
        borderLeft: selected ? `2px solid ${ACCENT}` : '2px solid transparent',
      }}
      title={`${httpDisplayName(span)}\n${span.service_name}\n${fmtNs(span.duration_ns)}`}
    >
      {/* name column */}
      <div
        className="flex items-center gap-[7px] min-w-0 overflow-hidden"
        style={{ paddingLeft: 14 + depth * 16 }}
      >
        {depth > 0 && <span className="w-2 h-px bg-border shrink-0 -ml-[7px]" />}
        {orphan && <AlertTriangle size={10} color="var(--warn)" className="shrink-0" />}
        <span
          className="w-[9px] h-[9px] rounded-sm opacity-85 shrink-0"
          style={{ background: c.fg }}
        />
        <span
          className="font-mono text-[10px] shrink-0 max-w-[175px] overflow-hidden text-ellipsis whitespace-nowrap opacity-75"
          style={{ color: c.fg }}
          title={span.service_name}
        >
          {span.service_name}
        </span>
        <span className="text-[10px] text-muted-foreground shrink-0 opacity-50">/</span>
        <span
          className="font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 min-w-0"
          style={{ color: isError ? 'var(--danger)' : 'var(--foreground)' }}
        >
          {httpDisplayName(span)}
        </span>
        {isN1 && (
          <span className="font-mono text-[9px] font-bold text-warn-ink uppercase tracking-[0.08em] shrink-0 px-1 py-px rounded-[3px] bg-warn-bg">
            n+1
          </span>
        )}
        {!isN1 && tag && (
          <span
            className="font-mono text-[9px] font-bold uppercase tracking-[0.08em] shrink-0 px-1 py-px rounded-[3px]"
            style={{
              color: tagColor,
              background: `color-mix(in oklch, ${tagColor} 13%, transparent)`,
            }}
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
          {[0, 1, 2, 3].map((i) => (
            <div
              key={i}
              className={`flex-1 ${i === 0 ? '' : 'border-l border-dashed border-border'}`}
            />
          ))}
        </div>
        {/* span bar */}
        <div
          style={{
            position: 'absolute',
            top: 4,
            height: 10,
            left: `${left}%`,
            width: `${width}%`,
            background: isError ? 'var(--danger-bg)' : c.bg,
            borderRadius: 3,
            boxShadow: isError
              ? `inset 0 0 0 1px color-mix(in oklch, var(--danger) 38%, transparent), inset 2px 0 0 var(--danger)`
              : `inset 0 0 0 1px ${c.fg}30, inset 2px 0 0 ${c.fg}`,
          }}
        />
        {/* warning outline for n+1 */}
        {(isN1 || tag === 'n+1') && (
          <div
            style={{
              position: 'absolute',
              top: 1,
              height: 16,
              left: `${left}%`,
              width: `${width}%`,
              borderRadius: 3,
              border: isN1 ? '1px solid var(--warn)' : '1px solid var(--danger)',
              boxShadow: isN1 ? '0 0 0 2px var(--warn-bg)' : '0 0 0 2px var(--danger-bg)',
              pointerEvents: 'none',
            }}
          />
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

function FlameView({
  flatSpans,
  traceStartNs,
  tags,
  selectedId,
  hoveredId,
  onSelect,
  onHover,
  zoom,
  onZoom,
}: {
  flatSpans: FlatSpan[]
  traceStartNs: number
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

  const tickStepMs =
    zDurMs > 400 ? 100 : zDurMs > 200 ? 50 : zDurMs > 80 ? 20 : zDurMs > 20 ? 10 : 5
  const tickStepNs = tickStepMs * 1_000_000
  const tickStart = Math.ceil(zStart / tickStepNs) * tickStepNs
  const ticks: number[] = []
  for (let t = tickStart; t <= zEnd; t += tickStepNs) ticks.push(t)

  const maxDepth = flatSpans.reduce((m, f) => Math.max(m, f.depth), 0)
  const FLAME_ROW = 26
  const totalH = (maxDepth + 1) * FLAME_ROW

  const visible = flatSpans.filter(
    ({ span }) => span.start_ns + span.duration_ns >= zStart && span.start_ns <= zEnd,
  )

  const hovFlat = hoveredId ? flatSpans.find((f) => f.span.span_id === hoveredId) : null

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* tick ruler */}
      <div className="relative h-6 border-b border-border bg-muted shrink-0">
        {ticks.map((t) => (
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
          const isSel = span.span_id === selectedId
          const isHov = span.span_id === hoveredId
          const isError = span.status_code === 2
          const hot = tag === 'n+1'

          const clipStart = Math.max(span.start_ns, zStart)
          const clipEnd = Math.min(span.end_ns, zEnd)
          const left = ((clipStart - zStart) / zDur) * 100
          const width = Math.max(0.2, ((clipEnd - clipStart) / zDur) * 100)
          const top = 6 + depth * FLAME_ROW

          return (
            <button
              key={span.span_id}
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                onSelect(span.span_id)
                onZoom([span.start_ns, span.end_ns])
              }}
              onMouseEnter={() => onHover(span.span_id)}
              className="absolute rounded-[3px] px-[5px] font-mono text-[10px] font-semibold text-left cursor-pointer overflow-hidden text-ellipsis whitespace-nowrap transition-[background] duration-[80ms] outline-none"
              style={{
                left: `${left}%`,
                width: `${width}%`,
                top,
                height: FLAME_ROW - 4,
                background:
                  isSel || isHov
                    ? `color-mix(in oklch, ${c.fg} 35%, ${c.bg})`
                    : isError
                      ? 'var(--danger-bg)'
                      : c.bg,
                color: c.fg,
                border: hot
                  ? '1px solid var(--danger)'
                  : isSel
                    ? `1px solid ${c.fg}`
                    : '1px solid transparent',
                boxShadow: isSel ? `inset 0 0 0 1px ${c.fg}` : 'none',
                transform: isHov ? 'translateY(-1px)' : 'none',
                zIndex: isHov || isSel ? 2 : 1,
              }}
              title={`${httpDisplayName(span)} · ${fmtNs(span.duration_ns)}`}
            >
              {width > 5 ? (
                <>
                  <span className="opacity-[0.65]">
                    {span.service_name.replace(/-service$/, '')}
                  </span>
                  {' · '}
                  <span>{httpDisplayName(span)}</span>
                </>
              ) : null}
            </button>
          )
        })}

        {/* hover tooltip */}
        {hovFlat &&
          (() => {
            const { span, depth } = hovFlat
            const c = svcColor(span.service_name)
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
                    <span className="ml-auto text-[9px] font-bold text-danger uppercase">
                      {tag}
                    </span>
                  )}
                </div>
                <div className="text-xs mb-1.5 overflow-hidden text-ellipsis whitespace-nowrap">
                  {httpDisplayName(span)}
                </div>
                <div className="flex gap-2.5 opacity-80">
                  <span>
                    dur <strong>{fmtNs(span.duration_ns)}</strong>
                  </span>
                  <span>
                    at <strong>+{fmtNs(span.start_ns - traceStartNs)}</strong>
                  </span>
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
          {fmtNs(zStart - traceStartNs)}–{fmtNs(zEnd - traceStartNs)} · {visible.length}/
          {flatSpans.length} spans
        </span>
      </div>
    </div>
  )
}

// ── LogsView ──────────────────────────────────────────────────────────────────

function LogsView({ logs }: { logs: Log[] }) {
  const [search, setSearch] = useState('')
  const [filterSev, setFilterSev] = useState<
    'ALL' | 'FATAL' | 'ERROR' | 'WARN' | 'INFO' | 'DEBUG' | 'TRACE'
  >('ALL')

  const matchesSevFilter = (severity: number, filter: typeof filterSev) => {
    if (filter === 'ALL') return true
    if (filter === 'FATAL') return severity >= 21
    if (filter === 'ERROR') return severity >= 17 && severity < 21
    if (filter === 'WARN') return severity >= 13 && severity < 17
    if (filter === 'INFO') return severity >= 9 && severity < 13
    if (filter === 'DEBUG') return severity >= 5 && severity < 9
    if (filter === 'TRACE') return severity < 5
    return true
  }

  const filtered = logs.filter((l) => {
    if (!matchesSevFilter(l.severity, filterSev)) return false
    if (search) {
      const q = search.toLowerCase()
      if (!l.body.toLowerCase().includes(q) && !l.service_name.toLowerCase().includes(q))
        return false
    }
    return true
  })

  const sevBodyColor = (n: number): string => {
    if (n >= 13) return 'var(--ink)'
    if (n >= 9) return 'var(--ink2)'
    return 'var(--ink3)'
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* filter bar */}
      <div className="flex items-center gap-2.5 px-3 py-2 bg-muted border-b border-border shrink-0 flex-wrap">
        <input
          type="text"
          placeholder="search logs…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-[220px] h-7 bg-background border border-border rounded-[5px] px-2.5 font-mono text-[11px] text-foreground outline-none"
        />
        <div className="flex items-center gap-1">
          {(['ALL', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'] as const).map((chip) => (
            <button
              key={chip}
              type="button"
              onClick={() => setFilterSev(chip)}
              className="transition-colors"
              style={{
                padding: '3px 10px',
                borderRadius: 12,
                fontFamily: 'var(--font-mono)',
                fontSize: 10,
                fontWeight: filterSev === chip ? 700 : 600,
                letterSpacing: '0.05em',
                textTransform: 'uppercase',
                cursor: 'pointer',
                border: filterSev === chip ? '1px solid var(--border)' : '1px solid transparent',
                background: filterSev === chip ? 'var(--background)' : 'transparent',
                color: filterSev === chip ? 'var(--foreground)' : 'var(--muted-foreground)',
              }}
            >
              {chip}
            </button>
          ))}
        </div>
        <div className="flex-1" />
        <span className="font-mono text-[10px] text-muted-foreground">{filtered.length} logs</span>
      </div>

      {/* logs list */}
      <div className="flex-1 overflow-y-auto">
        {filtered.length === 0 ? (
          <div className="flex items-center justify-center h-full text-muted-foreground font-mono text-[13px]">
            {logs.length === 0 ? 'No logs in this trace' : 'No logs matching filters'}
          </div>
        ) : (
          filtered.map((log, i) => {
            const c = svcColor(log.service_name)
            return (
              <div
                key={`${log.timestamp_ns}:${log.span_id}:${log.body.slice(0, 40)}`}
                className="grid items-center h-7 border-b border-line2 px-2 gap-1.5"
                style={{
                  gridTemplateColumns: '90px 52px 130px 1fr',
                  background: i % 2 === 0 ? 'var(--background)' : 'var(--muted)',
                }}
              >
                {/* timestamp */}
                <div className="font-mono text-[11px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap shrink-0">
                  {new Date(log.timestamp_ns / 1_000_000).toLocaleTimeString()}
                </div>

                {/* severity badge */}
                <div className="flex items-center">
                  <LogLevelBadge severity={log.severity} />
                </div>

                {/* service chip */}
                <div className="flex items-center gap-[5px] overflow-hidden min-w-0">
                  <span
                    className="w-[5px] h-[5px] rounded-full shrink-0"
                    style={{ background: c.fg }}
                  />
                  <span
                    title={log.service_name}
                    className="font-mono text-[10.5px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap"
                  >
                    {log.service_name}
                  </span>
                </div>

                {/* body */}
                <div
                  className="font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap"
                  style={{ color: sevBodyColor(log.severity) }}
                >
                  {log.body}
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// ── TraceMeta ─────────────────────────────────────────────────────────────────

function TraceMeta({
  rootName,
  traceId,
  traceDurNs,
  spanCount,
  serviceCount,
  errorCount,
  lintCount,
  view,
  onView,
  zoom,
  traceStartNs,
  traceEndNs,
  onResetZoom,
}: {
  rootName: string
  traceId: string
  traceDurNs: number
  spanCount: number
  serviceCount: number
  errorCount: number
  lintCount: number
  view: 'waterfall' | 'flame' | 'graph' | 'logs'
  onView: (v: 'waterfall' | 'flame' | 'graph' | 'logs') => void
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
            <span className="font-mono text-[9px] font-bold text-danger bg-danger-bg px-[5px] py-px rounded-[3px] uppercase">
              error
            </span>
          )}
          {lintCount > 0 && (
            <span className="font-mono text-[9px] text-warn bg-warn-bg px-[5px] py-px rounded-[3px] uppercase">
              {lintCount} lint
            </span>
          )}
        </div>
        <div className="font-mono text-[10px] text-muted-foreground">{traceId}</div>
      </div>

      {/* stats */}
      <StatBox label="total" value={fmtNs(traceDurNs)} />
      <StatBox label="spans" value={String(spanCount)} />
      <StatBox label="svcs" value={String(serviceCount)} />

      {/* view toggle pill */}
      <div className="flex items-center bg-muted rounded-lg p-[3px] border border-border gap-0.5">
        {(['waterfall', 'flame', 'graph', 'logs'] as const).map((v) => {
          const active = view === v
          return (
            <button
              key={v}
              type="button"
              onClick={() => onView(v)}
              className={`inline-flex items-center gap-[5px] px-2.5 py-1 rounded-md font-sans text-xs font-medium cursor-pointer outline-none whitespace-nowrap border ${active ? 'bg-background text-foreground border-border' : 'bg-transparent text-muted-foreground border-transparent'}`}
            >
              {v === 'waterfall' ? (
                <WaterfallIcon />
              ) : v === 'flame' ? (
                <FlameIcon />
              ) : v === 'graph' ? (
                <GraphIcon />
              ) : (
                <LogsIcon />
              )}
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
      <rect x="1" y="1.5" width="9" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="2.5" y="4.5" width="6" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="4" y="7.5" width="4" height="1.6" fill="currentColor" opacity="0.85" />
    </svg>
  )
}

function GraphIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <circle cx="6" cy="2" r="1.4" fill="currentColor" opacity="0.85" />
      <circle cx="2.5" cy="9" r="1.4" fill="currentColor" opacity="0.85" />
      <circle cx="9.5" cy="9" r="1.4" fill="currentColor" opacity="0.85" />
      <line x1="6" y1="3.4" x2="2.7" y2="7.8" stroke="currentColor" strokeWidth="1" opacity="0.6" />
      <line x1="6" y1="3.4" x2="9.3" y2="7.8" stroke="currentColor" strokeWidth="1" opacity="0.6" />
    </svg>
  )
}

function FlameIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1" y="1" width="10" height="2.6" fill="currentColor" opacity="0.45" />
      <rect x="1" y="4.2" width="5" height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="6.5" y="4.2" width="4" height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="1.5" y="7.4" width="3" height="2.6" fill="currentColor" opacity="0.9" />
    </svg>
  )
}

function LogsIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
      <line x1="2" y1="2" x2="10" y2="2" stroke="currentColor" strokeWidth="1" opacity="0.85" />
      <line x1="2" y1="5" x2="10" y2="5" stroke="currentColor" strokeWidth="1" opacity="0.75" />
      <line x1="2" y1="8" x2="8" y2="8" stroke="currentColor" strokeWidth="1" opacity="0.65" />
      <line x1="2" y1="11" x2="6" y2="11" stroke="currentColor" strokeWidth="1" opacity="0.55" />
    </svg>
  )
}

// ── N+1 Issue Banner ──────────────────────────────────────────────────────────

function N1Banner({
  issues,
}: {
  issues: { fingerprint: string; count: number; wastedNs: number }[]
}) {
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
        {issues.length > 1 && <span className="ml-2 opacity-70">+{issues.length - 1} more</span>}
      </span>
    </div>
  )
}

// ── TraceWaterfall (main export) ──────────────────────────────────────────────

interface Props {
  spans: Span[]
  warnings?: LintWarning[]
  issues?: TraceIssue[]
  logs?: Log[]
  traceId?: string
}

export default function TraceWaterfall({
  spans,
  warnings = [],
  issues = [],
  logs = [],
  traceId = '',
}: Props) {
  const traceStartNs = spans.reduce((m, s) => Math.min(m, s.start_ns), Infinity)
  const traceEndNs = spans.reduce((m, s) => Math.max(m, s.end_ns), 0)
  const traceDurNs = traceEndNs - traceStartNs

  // Deep-link: ?span=X (or legacy ?spanId=X) pre-selects a span; ?view= seeds the view.
  const urlParams =
    typeof window !== 'undefined'
      ? new URLSearchParams(window.location.search)
      : new URLSearchParams()
  const initialSelected = urlParams.get('span') ?? urlParams.get('spanId')
  const initialView =
    (['waterfall', 'flame', 'graph', 'logs'] as const).find((v) => v === urlParams.get('view')) ??
    'waterfall'

  const [view, setView] = useState<'waterfall' | 'flame' | 'graph' | 'logs'>(initialView)
  const [selectedId, setSelectedId] = useState<string | null>(initialSelected)
  const [hoveredId, setHoveredId] = useState<string | null>(null)
  const [zoom, setZoom] = useState<[number, number]>([traceStartNs, traceEndNs])

  const previousExtent = useRef<[number, number]>([traceStartNs, traceEndNs])
  useEffect(() => {
    const previous = previousExtent.current
    if (Number.isFinite(traceStartNs))
      setZoom((current) =>
        current[0] === previous[0] && current[1] === previous[1]
          ? [traceStartNs, traceEndNs]
          : current,
      )
    previousExtent.current = [traceStartNs, traceEndNs]
  }, [traceStartNs, traceEndNs])

  const resetZoom = useCallback(() => {
    setZoom([traceStartNs, traceEndNs])
  }, [traceStartNs, traceEndNs])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') resetZoom()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [resetZoom])

  const flatSpans = flatten(spans)
  const tags = buildTagMap(warnings)

  const n1SpanIds = useMemo(() => detectN1SpanIds(flatSpans, issues), [flatSpans, issues])
  const n1Banner = useMemo(() => n1BannerEntries(flatSpans, issues), [flatSpans, issues])

  const rootSpan = spans.find((s) => !s.parent_span_id || s.parent_span_id === ZERO_ID)
  const serviceCount = new Set(spans.map((s) => s.service_name)).size
  const errorCount = spans.filter((s) => s.status_code === 2).length
  const selectedSpan = selectedId ? (spans.find((s) => s.span_id === selectedId) ?? null) : null

  const handleSelect = useCallback((spanId: string) => {
    setSelectedId((prev) => (prev === spanId ? null : spanId))
  }, [])

  // Keep ?span= and ?view= in sync so the back button is not polluted.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (selectedId) {
      params.set('span', selectedId)
    } else {
      params.delete('span')
    }
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
    const idx = flatSpans.findIndex((f) => f.span.span_id === initialSelected)
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
        rootName={rootSpan ? httpDisplayName(rootSpan) : traceId}
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

      <div className="flex-1 min-h-0 flex overflow-x-auto overflow-y-hidden">
        {/* left: waterfall or flame */}
        <div
          className="flex-1 flex flex-col overflow-hidden"
          style={{ minWidth: view === 'waterfall' ? NAME_W + TIMELINE_MIN_W + DUR_W : 0 }}
        >
          {view === 'waterfall' ? (
            <>
              <Ruler traceDurNs={traceDurNs} spanCount={spans.length} />
              <div ref={parentRef} className="flex-1 overflow-y-auto overflow-x-hidden">
                <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
                  {virtualizer.getVirtualItems().map((vRow) => {
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
              tags={tags}
              selectedId={selectedId}
              hoveredId={hoveredId}
              onSelect={handleSelect}
              onHover={setHoveredId}
              zoom={zoom}
              onZoom={setZoom}
            />
          ) : view === 'graph' ? (
            <TraceGraph spans={spans} selectedId={selectedId} onSelect={handleSelect} />
          ) : (
            <LogsView logs={logs} />
          )}
        </div>

        {/* right: inspector — hidden for logs view */}
        {view !== 'logs' &&
          selectedSpan &&
          (() => {
            const selIsN1 = n1SpanIds.has(selectedSpan.span_id)
            // Compute count for the selected span's db.statement
            let selN1Count = 0
            if (selIsN1) {
              let attrs: Record<string, unknown> = {}
              try {
                attrs = JSON.parse(selectedSpan.attributes ?? '{}')
              } catch {
                /* empty */
              }
              const stmt = attrs['db.statement'] || attrs['db.query.text']
              if (typeof stmt === 'string') {
                for (const flat of flatSpans) {
                  let a: Record<string, unknown> = {}
                  try {
                    a = JSON.parse(flat.span.attributes ?? '{}')
                  } catch {
                    /* empty */
                  }
                  if (
                    flat.span.parent_span_id === selectedSpan.parent_span_id &&
                    (a['db.statement'] || a['db.query.text']) === stmt
                  )
                    selN1Count++
                }
              }
            }
            return (
              <SpanInspector
                span={selectedSpan}
                spans={spans}
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
