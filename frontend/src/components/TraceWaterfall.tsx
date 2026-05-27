import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { AlertTriangle, X } from 'lucide-react'
import { Span, LintWarning, TraceIssue, Log, api } from '@/lib/api'
import { SPAN_PALETTE as PALETTE, SPAN_ACCENT as ACCENT, svcColor, flatten, fmtNs, KIND_LABELS, buildTagMap, FlatSpan } from '@/lib/span-utils'

// ── layout constants ──────────────────────────────────────────────────────────

const NAME_W = 320
const ZERO_ID = '0000000000000000'
const DUR_W = 56
const ROW_H = 32
const STEPS = 6

// ── Ruler ─────────────────────────────────────────────────────────────────────

function Ruler({ traceDurNs, spanCount }: { traceDurNs: number; spanCount: number }) {
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: `${NAME_W}px 1fr ${DUR_W}px`,
      alignItems: 'center',
      borderBottom: '1px solid var(--border)',
      background: 'var(--muted)',
      flexShrink: 0,
    }}>
      <div style={{
        padding: '7px 14px',
        fontFamily: 'var(--font-mono)',
        fontSize: 9,
        textTransform: 'uppercase' as const,
        letterSpacing: '0.14em',
        color: 'var(--muted-foreground)',
      }}>
        span · {spanCount}
      </div>
      <div style={{
        position: 'relative' as const,
        height: 24,
        marginLeft: 8,
        marginRight: 8,
        borderLeft: '1px solid var(--border)',
      }}>
        {Array.from({ length: STEPS + 1 }).map((_, i) => (
          <span key={i} style={{
            position: 'absolute' as const,
            left: `${(i / STEPS) * 100}%`,
            top: 0,
            height: '100%',
            borderLeft: i === 0 ? 'none' : '1px solid var(--border)',
            transform: 'translateX(-1px)',
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            color: 'var(--muted-foreground)',
            padding: '7px 4px',
            whiteSpace: 'nowrap' as const,
          }}>
            {i === 0 ? '' : fmtNs((traceDurNs / STEPS) * i)}
          </span>
        ))}
      </div>
      <div style={{
        padding: '7px 10px 7px 0',
        textAlign: 'right' as const,
        fontFamily: 'var(--font-mono)',
        fontSize: 9,
        color: 'var(--muted-foreground)',
        textTransform: 'uppercase' as const,
        letterSpacing: '0.14em',
      }}>
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
    <div style={{
      height: 54,
      padding: '10px 16px',
      background: 'var(--muted)',
      borderBottom: '1px solid var(--border)',
      position: 'relative' as const,
      flexShrink: 0,
    }}>
      <div style={{
        position: 'absolute' as const,
        left: 16, right: 16, top: 14, bottom: 10,
        background: 'var(--background)',
        borderRadius: 3,
        overflow: 'hidden' as const,
      }}>
        {flatSpans.map(({ span, depth }) => {
          const c = svcColor(span.service_name)
          return (
            <div key={span.span_id} style={{
              position: 'absolute' as const,
              left: `${((span.start_ns - traceStartNs) / traceDurNs) * 100}%`,
              width: `${Math.max(0.3, (span.duration_ns / traceDurNs) * 100)}%`,
              top: depth * 4,
              height: 3,
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

function SpanRow({ flat, traceStartNs, traceDurNs, selected, hovered, tag, isN1, onSelect, onHover }: {
  flat: FlatSpan
  traceStartNs: number
  traceDurNs: number
  selected: boolean
  hovered: boolean
  tag: string | undefined
  isN1: boolean
  onSelect: () => void
  onHover: (id: string | null) => void
}) {
  const { span, depth, orphan } = flat
  const c = svcColor(span.service_name)
  const isError = span.status_code === 2
  const tagColor = isN1 ? 'var(--warn)' : tag === 'n+1' ? '#ef4444' : '#f59e0b'

  const left  = traceDurNs > 0 ? ((span.start_ns - traceStartNs) / traceDurNs) * 100 : 0
  const width = traceDurNs > 0 ? Math.max(0.6, (span.duration_ns / traceDurNs) * 100) : 0.6

  return (
    <div
      onClick={onSelect}
      onMouseEnter={() => onHover(span.span_id)}
      onMouseLeave={() => onHover(null)}
      style={{
        display: 'grid',
        gridTemplateColumns: `${NAME_W}px 1fr ${DUR_W}px`,
        alignItems: 'center',
        height: ROW_H,
        cursor: 'pointer',
        background: selected
          ? `${ACCENT}1a`
          : hovered ? `${ACCENT}0d` : 'transparent',
        borderLeft: selected ? `2px solid ${ACCENT}` : '2px solid transparent',
        borderBottom: '1px solid var(--border)',
        transition: 'background 0.08s',
      }}
      title={`${span.name}\n${span.service_name}\n${fmtNs(span.duration_ns)}`}
    >
      {/* name column */}
      <div style={{
        paddingLeft: 14 + depth * 16,
        display: 'flex',
        alignItems: 'center',
        gap: 7,
        minWidth: 0,
        overflow: 'hidden',
      }}>
        {depth > 0 && (
          <span style={{
            width: 8, height: 1,
            background: 'var(--border)',
            flex: '0 0 auto',
            marginLeft: -7,
          }} />
        )}
        {orphan && <AlertTriangle size={10} color="#f59e0b" style={{ flexShrink: 0 }} />}
        <span style={{
          width: 9, height: 9,
          borderRadius: 2,
          background: c.fg,
          opacity: 0.85,
          flex: '0 0 auto',
        }} />
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          color: isError ? '#ef4444' : 'var(--foreground)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flex: '1 1 auto',
          minWidth: 0,
        }}>
          {span.name}
        </span>
        {isN1 && (
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            fontWeight: 700,
            color: 'var(--warn-ink)',
            textTransform: 'uppercase',
            letterSpacing: '0.08em',
            flex: '0 0 auto',
            padding: '1px 4px',
            borderRadius: 3,
            background: 'var(--warn-bg)',
          }}>
            n+1
          </span>
        )}
        {!isN1 && tag && (
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 9,
            fontWeight: 700,
            color: tagColor,
            textTransform: 'uppercase',
            letterSpacing: '0.08em',
            flex: '0 0 auto',
            padding: '1px 4px',
            borderRadius: 3,
            background: tagColor + '22',
          }}>
            {tag}
          </span>
        )}
      </div>

      {/* timeline column */}
      <div style={{ position: 'relative', height: 18, marginLeft: 8, marginRight: 8 }}>
        {/* dashed grid lines */}
        <div style={{ position: 'absolute', inset: 0, display: 'flex' }}>
          {[0, 1, 2, 3].map(i => (
            <div key={i} style={{
              flex: 1,
              borderLeft: i === 0 ? 'none' : '1px dashed var(--border)',
            }} />
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
      <div style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 10,
        color: 'var(--muted-foreground)',
        textAlign: 'right',
        paddingRight: 10,
      }}>
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
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* tick ruler */}
      <div style={{
        position: 'relative', height: 24,
        borderBottom: '1px solid var(--border)',
        background: 'var(--muted)',
        flexShrink: 0,
      }}>
        {ticks.map(t => (
          <span key={t} style={{
            position: 'absolute',
            left: `${((t - zStart) / zDur) * 100}%`,
            top: 0, bottom: 0,
            borderLeft: '1px solid var(--border)',
            fontFamily: 'var(--font-mono)', fontSize: 9,
            color: 'var(--muted-foreground)',
            padding: '7px 4px', whiteSpace: 'nowrap',
            transform: 'translateX(-1px)',
          }}>
            +{fmtNs(t - traceStartNs)}
          </span>
        ))}
      </div>

      {/* flame canvas */}
      <div
        onMouseLeave={() => onHover(null)}
        style={{
          position: 'relative', flex: 1, overflow: 'auto',
          minHeight: totalH + 12, padding: '6px 0',
        }}
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
              style={{
                position: 'absolute',
                left: `${left}%`, width: `${width}%`,
                top, height: FLAME_ROW - 4,
                background: isSel || isHov
                  ? `color-mix(in oklch, ${c.fg} 35%, ${c.bg})`
                  : isError ? '#ef444418' : c.bg,
                color: c.fg,
                border: hot ? '1px solid #ef4444' : isSel ? `1px solid ${c.fg}` : '1px solid transparent',
                borderRadius: 3,
                padding: '0 5px',
                fontFamily: 'var(--font-mono)', fontSize: 10,
                fontWeight: 600, textAlign: 'left',
                cursor: 'pointer',
                overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                boxShadow: isSel ? `inset 0 0 0 1px ${c.fg}` : 'none',
                transition: 'background 0.08s',
                transform: isHov ? 'translateY(-1px)' : 'none',
                zIndex: isHov || isSel ? 2 : 1,
                outline: 'none',
              }}
              title={`${span.name} · ${fmtNs(span.duration_ns)}`}
            >
              {width > 5 ? (
                <>
                  <span style={{ opacity: 0.65 }}>{span.service_name.replace(/-service$/, '')}</span>
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
            <div style={{
              position: 'absolute',
              left: `clamp(8px, ${leftPct}%, calc(100% - 248px))`,
              top, zIndex: 5,
              background: 'var(--foreground)', color: 'var(--background)',
              fontFamily: 'var(--font-mono)', fontSize: 11,
              padding: '8px 10px', borderRadius: 6,
              minWidth: 220, maxWidth: 280,
              pointerEvents: 'none',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
                <span style={{ width: 6, height: 6, background: c.fg, borderRadius: 6 }} />
                <span style={{ fontSize: 9, textTransform: 'uppercase', letterSpacing: '0.14em', opacity: 0.65 }}>
                  {span.service_name}
                </span>
                {tag && (
                  <span style={{ marginLeft: 'auto', fontSize: 9, fontWeight: 700, color: '#ff9b8a', textTransform: 'uppercase' }}>
                    {tag}
                  </span>
                )}
              </div>
              <div style={{ fontSize: 12, marginBottom: 6, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {span.name}
              </div>
              <div style={{ display: 'flex', gap: 10, opacity: 0.8 }}>
                <span>dur <strong>{fmtNs(span.duration_ns)}</strong></span>
                <span>at <strong>+{fmtNs(span.start_ns - traceStartNs)}</strong></span>
              </div>
            </div>
          )
        })()}
      </div>

      {/* footer hint */}
      <div style={{
        padding: '5px 14px',
        borderTop: '1px solid var(--border)',
        background: 'var(--muted)',
        fontFamily: 'var(--font-mono)', fontSize: 9.5,
        color: 'var(--muted-foreground)',
        display: 'flex', gap: 16, flexShrink: 0,
      }}>
        <span>click → zoom & select</span>
        <span>esc → reset zoom</span>
        <span style={{ flex: 1 }} />
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
      <div style={{ padding: '14px 16px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink3)' }}>
        …
      </div>
    )
  }

  if (logs.length === 0) {
    return (
      <div style={{ padding: '14px 16px', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--ink3)' }}>
        no logs for this span
      </div>
    )
  }

  return (
    <div style={{ flex: 1, overflow: 'auto' }}>
      {logs.map((log, i) => (
        <div key={i} style={{
          padding: '4px 10px',
          display: 'flex', alignItems: 'flex-start', gap: 7,
          borderBottom: '1px solid var(--line2)',
        }}>
          {/* severity dot */}
          <span style={{
            width: 5, height: 5,
            borderRadius: '50%',
            background: inspSevColor(log.severity),
            flexShrink: 0,
            marginTop: 4,
          }} />
          {/* timestamp */}
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10,
            color: 'var(--ink3)',
            flexShrink: 0,
            whiteSpace: 'nowrap',
          }}>
            {inspFmtRelative(log.timestamp_ns)}
          </span>
          {/* body */}
          <span style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 10.5,
            color: inspBodyColor(log.severity),
            lineHeight: 1.4,
            wordBreak: 'break-word',
            minWidth: 0,
          }}>
            {log.body}
          </span>
        </div>
      ))}
    </div>
  )
}

// ── Inspector ─────────────────────────────────────────────────────────────────

function Inspector({ span, traceStartNs, warnings, isN1, n1Count, onClose }: {
  span: Span
  traceStartNs: number
  warnings: LintWarning[]
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
    <aside style={{
      width: 320,
      borderLeft: '1px solid var(--border)',
      background: 'var(--background)',
      display: 'flex', flexDirection: 'column',
      overflow: 'hidden', flexShrink: 0,
    }}>
      {/* header */}
      <div style={{ padding: '14px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <span style={{ width: 9, height: 9, borderRadius: 2, background: c.fg, opacity: 0.85 }} />
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: c.fg }}>
            {span.service_name}
          </span>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9,
            color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.12em',
          }}>
            {kind}
          </span>
          <div style={{ flex: 1 }} />
          <button
            onClick={onClose}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              color: 'var(--muted-foreground)', padding: 2, borderRadius: 3,
              display: 'flex', alignItems: 'center',
            }}
          >
            <X size={13} />
          </button>
        </div>
        <div style={{
          fontFamily: 'var(--font-mono)', fontSize: 12,
          color: isError ? '#ef4444' : 'var(--foreground)',
          lineHeight: 1.4, wordBreak: 'break-word', marginBottom: 12,
        }}>
          {span.name}
        </div>
        <div style={{ display: 'flex', gap: 16, alignItems: 'baseline' }}>
          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9,
              color: 'var(--muted-foreground)',
              textTransform: 'uppercase', letterSpacing: '0.14em', marginBottom: 2,
            }}>
              duration
            </div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 19,
              fontWeight: 600, color: 'var(--foreground)',
            }}>
              {fmtNs(span.duration_ns)}
            </div>
          </div>
          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9,
              color: 'var(--muted-foreground)',
              textTransform: 'uppercase', letterSpacing: '0.14em', marginBottom: 2,
            }}>
              started
            </div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--foreground)' }}>
              +{fmtNs(span.start_ns - traceStartNs)}
            </div>
          </div>
        </div>
      </div>

      {/* N+1 callout */}
      {isN1 && (
        <div style={{ margin: '12px 14px 0' }}>
          <div style={{
            padding: '10px 12px',
            background: 'var(--warn-bg)',
            border: '1px solid var(--warn)',
            borderRadius: 7,
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
              <AlertTriangle size={11} color="var(--warn)" style={{ flexShrink: 0 }} />
              <span style={{
                fontFamily: 'var(--font-mono)', fontSize: 9.5,
                fontWeight: 700, color: 'var(--warn)', letterSpacing: '0.06em',
              }}>
                n_plus_one
              </span>
            </div>
            <div style={{ fontSize: 11, color: 'var(--warn-ink)', lineHeight: 1.45 }}>
              This DB span is repeated <strong>{n1Count}×</strong> in the trace — likely an N+1 query pattern.
            </div>
          </div>
        </div>
      )}

      {/* warning callouts */}
      {spanWarnings.length > 0 && (
        <div style={{ margin: '12px 14px 0' }}>
          {spanWarnings.map((w, i) => (
            <div key={i} style={{
              padding: '10px 12px',
              marginBottom: i < spanWarnings.length - 1 ? 6 : 0,
              background: 'color-mix(in oklch, #ef4444 10%, var(--background))',
              border: '1px solid color-mix(in oklch, #ef4444 30%, var(--background))',
              borderRadius: 7,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
                <span style={{ width: 6, height: 6, borderRadius: 6, background: '#ef4444', flexShrink: 0 }} />
                <span style={{
                  fontFamily: 'var(--font-mono)', fontSize: 9.5,
                  fontWeight: 700, color: '#ef4444', letterSpacing: '0.06em',
                }}>
                  {w.rule_id}
                </span>
              </div>
              <div style={{ fontSize: 11, color: 'var(--foreground)', lineHeight: 1.45 }}>
                {w.message}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* tab bar */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        borderBottom: '1px solid var(--line)',
        padding: '0 14px',
        fontFamily: 'var(--font-mono)',
        fontSize: 11,
        flexShrink: 0,
      }}>
        <button type="button" onClick={() => setActiveTab('attrs')} style={tabStyle('attrs')}>attrs</button>
        <button type="button" onClick={() => setActiveTab('logs')}  style={tabStyle('logs')}>logs</button>
      </div>

      {/* attrs tab */}
      {activeTab === 'attrs' && (
        <div style={{ flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
          {attrEntries.length > 0 && (
            <>
              <div style={{
                padding: '12px 14px 5px',
                display: 'flex', alignItems: 'center',
                fontFamily: 'var(--font-mono)', fontSize: 9,
                textTransform: 'uppercase', letterSpacing: '0.14em',
                color: 'var(--muted-foreground)',
              }}>
                attributes
                <span style={{ flex: 1 }} />
                <span style={{ textTransform: 'none', letterSpacing: 0 }}>{attrEntries.length}</span>
              </div>
              <AttrGrid entries={attrEntries} />
            </>
          )}
          {resEntries.length > 0 && (
            <>
              <div style={{
                padding: '12px 14px 5px',
                fontFamily: 'var(--font-mono)', fontSize: 9,
                textTransform: 'uppercase', letterSpacing: '0.14em',
                color: 'var(--muted-foreground)',
              }}>
                resource
              </div>
              <AttrGrid entries={resEntries} />
            </>
          )}
          {attrEntries.length === 0 && resEntries.length === 0 && (
            <div style={{
              padding: '18px 16px',
              fontFamily: 'var(--font-mono)', fontSize: 11,
              color: 'var(--muted-foreground)',
            }}>
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
    <div style={{ padding: '0 6px 6px' }}>
      {entries.map(([k, v]) => (
        <div key={k} style={{
          display: 'grid', gridTemplateColumns: '1fr 1.4fr', gap: 8,
          padding: '4px 8px', borderRadius: 4,
        }}>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 10.5,
            color: 'var(--muted-foreground)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>
            {k}
          </div>
          <div style={{
            fontFamily: 'var(--font-mono)', fontSize: 10.5,
            color: 'var(--foreground)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}
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
  view: 'waterfall' | 'flame'
  onView: (v: 'waterfall' | 'flame') => void
  zoom: [number, number]
  traceStartNs: number
  traceEndNs: number
  onResetZoom: () => void
}) {
  const isZoomed = zoom[0] > traceStartNs || zoom[1] < traceEndNs

  return (
    <div style={{
      padding: '12px 18px',
      borderBottom: '1px solid var(--border)',
      background: 'var(--background)',
      display: 'flex', alignItems: 'center', gap: 16,
      flexShrink: 0,
    }}>
      <div style={{ minWidth: 0, flex: '1 1 auto' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4, flexWrap: 'wrap' }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 13, fontWeight: 600,
            color: 'var(--foreground)',
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>
            {rootName}
          </span>
          {errorCount > 0 && (
            <span style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, fontWeight: 700,
              color: '#ef4444', background: '#ef444420',
              padding: '1px 5px', borderRadius: 3, textTransform: 'uppercase',
            }}>
              error
            </span>
          )}
          {lintCount > 0 && (
            <span style={{
              fontFamily: 'var(--font-mono)', fontSize: 9,
              color: '#f59e0b', background: '#f59e0b20',
              padding: '1px 5px', borderRadius: 3, textTransform: 'uppercase',
            }}>
              {lintCount} lint
            </span>
          )}
        </div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)' }}>
          {traceId}
        </div>
      </div>

      {/* stats */}
      <StatBox label="total"  value={fmtNs(traceDurNs)} />
      <StatBox label="spans"  value={String(spanCount)} />
      <StatBox label="svcs"   value={String(serviceCount)} />

      {/* view toggle pill */}
      <div style={{
        display: 'flex', alignItems: 'center',
        background: 'var(--muted)', borderRadius: 8,
        padding: 3, border: '1px solid var(--border)', gap: 2,
      }}>
        {(['waterfall', 'flame'] as const).map(v => {
          const active = view === v
          return (
            <button key={v} type="button" onClick={() => onView(v)} style={{
              display: 'inline-flex', alignItems: 'center', gap: 5,
              padding: '4px 10px', borderRadius: 6,
              background: active ? 'var(--background)' : 'transparent',
              color: active ? 'var(--foreground)' : 'var(--muted-foreground)',
              border: active ? '1px solid var(--border)' : '1px solid transparent',
              fontFamily: 'var(--font-sans)', fontSize: 12, fontWeight: 500,
              cursor: 'pointer', outline: 'none', whiteSpace: 'nowrap',
            }}>
              {v === 'waterfall' ? <WaterfallIcon /> : <FlameIcon />}
              {v.charAt(0).toUpperCase() + v.slice(1)}
            </button>
          )
        })}
      </div>

      {/* reset zoom */}
      {isZoomed && (
        <button type="button" onClick={onResetZoom} style={{
          padding: '4px 10px', borderRadius: 6,
          background: `${ACCENT}18`,
          border: `1px solid ${ACCENT}`,
          color: ACCENT,
          fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 600,
          cursor: 'pointer', outline: 'none', whiteSpace: 'nowrap',
        }}>
          ↺ reset zoom
        </button>
      )}
    </div>
  )
}

function StatBox({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ flexShrink: 0 }}>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 9,
        textTransform: 'uppercase', letterSpacing: '0.14em',
        color: 'var(--muted-foreground)',
      }}>
        {label}
      </div>
      <div style={{
        fontFamily: 'var(--font-mono)', fontWeight: 600,
        fontSize: 14, color: 'var(--foreground)',
        lineHeight: 1.1, marginTop: 2,
      }}>
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
    <div style={{
      margin: '0 0 0 0',
      padding: '9px 16px',
      borderBottom: '1px solid var(--border)',
      background: 'var(--warn-bg)',
      borderLeft: '3px solid var(--warn)',
      display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
    }}>
      <AlertTriangle size={13} color="var(--warn)" style={{ flexShrink: 0 }} />
      <span style={{
        fontFamily: 'var(--font-mono)', fontSize: 11,
        color: 'var(--warn-ink)', flex: 1, minWidth: 0,
      }}>
        <strong>N+1 detected</strong>
        {' — '}
        <span style={{ opacity: 0.85 }}>{fp}</span>
        {' called '}
        <strong>{top.count}×</strong>
        {' — '}
        <strong>{fmtNs(top.wastedNs)}</strong>
        {' wasted'}
        {issues.length > 1 && (
          <span style={{ marginLeft: 8, opacity: 0.7 }}>+{issues.length - 1} more</span>
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

  const [view,       setView]       = useState<'waterfall' | 'flame'>('waterfall')
  const [selectedId, setSelectedId] = useState<string | null>(null)
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

  // Client-side N+1 detection: group DB spans by raw db.statement and mark those
  // with 10+ occurrences. Also incorporate server-detected issues.
  const n1SpanIds = useMemo(() => {
    const set = new Set<string>()
    // From server issues: look up example_span_id and all spans sharing parent
    for (const issue of issues) {
      if (issue.kind !== 'n_plus_one') continue
      if (issue.example_span_id) set.add(issue.example_span_id)
    }
    // Client-side: count by raw db.statement
    const counts = new Map<string, string[]>()
    for (const flat of flatSpans) {
      const s = flat.span
      let attrs: Record<string, unknown> = {}
      try { attrs = JSON.parse(s.attributes ?? '{}') } catch { /* empty */ }
      const stmt = attrs['db.statement']
      if (typeof stmt !== 'string' || !stmt) continue
      const ids = counts.get(stmt) ?? []
      ids.push(s.span_id)
      counts.set(stmt, ids)
    }
    for (const [, ids] of counts) {
      if (ids.length >= 10) {
        for (const id of ids) set.add(id)
      }
    }
    return set
  }, [flatSpans, issues])

  // Build N+1 summary for the banner (from server issues, falling back to client-side counts)
  const n1Banner = useMemo(() => {
    if (issues.length > 0) {
      return issues
        .filter(i => i.kind === 'n_plus_one')
        .map(i => ({ fingerprint: i.fingerprint, count: i.count, wastedNs: i.wasted_ns }))
        .sort((a, b) => b.wastedNs - a.wastedNs)
    }
    // Fallback: build from client-side counts
    const counts = new Map<string, { ids: string[]; totalNs: number }>()
    for (const flat of flatSpans) {
      const s = flat.span
      let attrs: Record<string, unknown> = {}
      try { attrs = JSON.parse(s.attributes ?? '{}') } catch { /* empty */ }
      const stmt = attrs['db.statement']
      if (typeof stmt !== 'string' || !stmt) continue
      const entry = counts.get(stmt) ?? { ids: [], totalNs: 0 }
      entry.ids.push(s.span_id)
      entry.totalNs += s.duration_ns
      counts.set(stmt, entry)
    }
    const result: { fingerprint: string; count: number; wastedNs: number }[] = []
    for (const [fp, { ids, totalNs }] of counts) {
      if (ids.length >= 10) result.push({ fingerprint: fp, count: ids.length, wastedNs: totalNs })
    }
    return result.sort((a, b) => b.wastedNs - a.wastedNs)
  }, [issues, flatSpans])

  const rootSpan     = spans.find(s => !s.parent_span_id || s.parent_span_id === ZERO_ID)
  const serviceCount = new Set(spans.map(s => s.service_name)).size
  const errorCount   = spans.filter(s => s.status_code === 2).length
  const selectedSpan = selectedId ? (spans.find(s => s.span_id === selectedId) ?? null) : null

  const handleSelect = useCallback((spanId: string) => {
    setSelectedId(prev => prev === spanId ? null : spanId)
  }, [])

  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: flatSpans.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_H,
    overscan: 10,
  })

  if (spans.length === 0) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--muted-foreground)', fontFamily: 'var(--font-mono)', fontSize: 13 }}>
        No spans
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
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

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* left: waterfall or flame */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {view === 'waterfall' ? (
            <>
              <Ruler traceDurNs={traceDurNs} spanCount={spans.length} />
              <div ref={parentRef} style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden' }}>
                <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
                  {virtualizer.getVirtualItems().map(vRow => {
                    const flat = flatSpans[vRow.index]
                    return (
                      <div key={flat.span.span_id} style={{
                        position: 'absolute', top: vRow.start, left: 0, right: 0, height: ROW_H,
                      }}>
                        <SpanRow
                          flat={flat}
                          traceStartNs={traceStartNs}
                          traceDurNs={traceDurNs}
                          selected={flat.span.span_id === selectedId}
                          hovered={flat.span.span_id === hoveredId}
                          tag={tags.get(flat.span.span_id)}
                          isN1={n1SpanIds.has(flat.span.span_id)}
                          onSelect={() => handleSelect(flat.span.span_id)}
                          onHover={setHoveredId}
                        />
                      </div>
                    )
                  })}
                </div>
              </div>
            </>
          ) : (
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
