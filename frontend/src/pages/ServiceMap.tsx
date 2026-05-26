import { useEffect, useRef, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ServiceMapData, ServiceMapNode, ServiceMapEdge } from '@/lib/api'
import { useWS } from '@/lib/ws'

// ── palette (same hash as TraceWaterfall) ─────────────────────────────────────

// Drift-aligned palette — sky, sage, amber, lavender, slate, mint, sand…
const PALETTE = [
  '#7aa3c4', '#88b29a', '#d6b46a', '#a08cc8',
  '#6a98b8', '#8ab8a0', '#c8a870', '#7898c0',
  '#d69882', '#98a8c0',
]

function svcColor(name: string): { fg: string; bg: string } {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (Math.imul(31, h) + name.charCodeAt(i)) | 0
  const fg = PALETTE[Math.abs(h) % PALETTE.length]
  return { fg, bg: fg + '28' }
}

// ── layout: layered left-to-right ─────────────────────────────────────────────

const NODE_W = 148
const NODE_H = 48
const GAP_X  = 72
const GAP_Y  = 22
const PAD    = 40

function computeLayout(
  nodeIds: string[],
  edges: { from: string; to: string }[],
): Map<string, { x: number; y: number }> {
  const outgoing = new Map<string, string[]>()
  const inDegree  = new Map<string, number>()
  for (const n of nodeIds) { outgoing.set(n, []); inDegree.set(n, 0) }
  for (const e of edges) {
    outgoing.get(e.from)?.push(e.to)
    inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1)
  }

  // longest-path layering via BFS from roots
  const layer = new Map<string, number>()
  const queue: string[] = []
  for (const n of nodeIds) {
    if ((inDegree.get(n) ?? 0) === 0) { layer.set(n, 0); queue.push(n) }
  }
  const visited = new Set<string>()
  while (queue.length > 0) {
    const n = queue.shift()!
    if (visited.has(n)) continue
    visited.add(n)
    for (const child of outgoing.get(n) ?? []) {
      const l = Math.max(layer.get(child) ?? 0, (layer.get(n) ?? 0) + 1)
      layer.set(child, l)
      queue.push(child)
    }
  }
  for (const n of nodeIds) if (!layer.has(n)) layer.set(n, 0)

  // group by layer
  const byLayer = new Map<number, string[]>()
  for (const [n, l] of layer) {
    if (!byLayer.has(l)) byLayer.set(l, [])
    byLayer.get(l)!.push(n)
  }

  const positions = new Map<string, { x: number; y: number }>()
  for (const [l, nodes] of byLayer) {
    const x = PAD + l * (NODE_W + GAP_X)
    nodes.forEach((n, i) => {
      positions.set(n, { x, y: PAD + i * (NODE_H + GAP_Y) })
    })
  }
  return positions
}

function canvasSize(positions: Map<string, { x: number; y: number }>) {
  let maxX = 0, maxY = 0
  for (const { x, y } of positions.values()) {
    maxX = Math.max(maxX, x + NODE_W + PAD)
    maxY = Math.max(maxY, y + NODE_H + PAD)
  }
  return { w: Math.max(maxX, 400), h: Math.max(maxY, 200) }
}

// ── formatting ────────────────────────────────────────────────────────────────

function fmtNs(ns: number): string {
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(0)}µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`
  return `${(ns / 1_000_000_000).toFixed(2)}s`
}

// ── SVG pieces ────────────────────────────────────────────────────────────────

function SvgEdge({ edge, pos, hot }: {
  edge: ServiceMapEdge
  pos: Map<string, { x: number; y: number }>
  hot: boolean
}) {
  const A = pos.get(edge.from), B = pos.get(edge.to)
  if (!A || !B) return null

  const x1 = A.x + NODE_W, y1 = A.y + NODE_H / 2
  const x2 = B.x,          y2 = B.y + NODE_H / 2
  const mx = (x1 + x2) / 2
  const stroke = hot ? '#ef4444' : 'var(--muted-foreground)'
  const sw = Math.min(4, 0.8 + edge.call_count * 0.6)
  const midX = (x1 + x2) / 2
  const midY = (y1 + y2) / 2 - 4

  return (
    <g>
      <defs>
        <marker
          id={`arrow-${hot ? 'hot' : 'norm'}`}
          markerWidth="6" markerHeight="6"
          refX="5" refY="3"
          orient="auto"
        >
          <path d="M0,0 L0,6 L6,3 z" fill={stroke} opacity={hot ? 0.85 : 0.5} />
        </marker>
      </defs>
      <path
        d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
        fill="none"
        stroke={stroke}
        strokeWidth={sw}
        opacity={hot ? 0.85 : 0.5}
        markerEnd={`url(#arrow-${hot ? 'hot' : 'norm'})`}
      />
      <text
        x={midX} y={midY}
        textAnchor="middle"
        fontFamily="var(--font-mono)" fontSize="9"
        fill="var(--muted-foreground)"
      >
        {edge.call_count}× · {fmtNs(edge.avg_duration_ns)}
      </text>
    </g>
  )
}

function SvgNode({ node, pos, selected, onClick }: {
  node: ServiceMapNode
  pos: Map<string, { x: number; y: number }>
  selected: boolean
  onClick: () => void
}) {
  const p = pos.get(node.id)
  if (!p) return null
  const c = svcColor(node.id)
  const hasError = node.error_count > 0
  const errRate = node.span_count > 0 ? node.error_count / node.span_count : 0

  return (
    <g
      transform={`translate(${p.x}, ${p.y})`}
      onClick={onClick}
      style={{ cursor: 'pointer' }}
    >
      <rect
        width={NODE_W} height={NODE_H}
        rx="10" ry="10"
        fill={c.bg}
        stroke={selected ? c.fg : c.fg}
        strokeOpacity={selected ? 0.9 : 0.35}
        strokeWidth={selected ? 2 : 1.5}
        filter={selected ? `drop-shadow(0 0 6px ${c.fg}50)` : undefined}
      />
      {/* left colour pip */}
      <circle cx="14" cy={NODE_H / 2} r="5" fill={c.fg} opacity="0.85" />
      {/* service name */}
      <text
        x="26" y="20"
        fontFamily="var(--font-mono)" fontSize="11" fontWeight="600"
        fill={c.fg}
      >
        {node.id}
      </text>
      {/* stats line */}
      <text
        x="26" y="36"
        fontFamily="var(--font-mono)" fontSize="9"
        fill={c.fg} opacity="0.65"
      >
        {node.span_count} spans
        {hasError ? ` · ${(errRate * 100).toFixed(0)}% err` : ''}
      </text>
      {/* error dot */}
      {hasError && (
        <circle cx={NODE_W - 10} cy="10" r="4" fill="#ef4444" opacity="0.85" />
      )}
    </g>
  )
}

// ── Empty state ───────────────────────────────────────────────────────────────

function Empty() {
  return (
    <div style={{
      flex: 1, display: 'flex', flexDirection: 'column',
      alignItems: 'center', justifyContent: 'center', gap: 12,
      color: 'var(--muted-foreground)',
    }}>
      <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
        <circle cx="16" cy="24" r="7" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
        <circle cx="36" cy="12" r="5" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
        <circle cx="36" cy="36" r="5" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
        <line x1="23" y1="20" x2="31" y2="14" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
        <line x1="23" y1="28" x2="31" y2="34" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
      </svg>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 13 }}>
        No spans yet
      </div>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, opacity: 0.6, textAlign: 'center', maxWidth: 280 }}>
        Send traces via OTLP and the service map will appear automatically.
      </div>
    </div>
  )
}

// ── ServiceMap page ───────────────────────────────────────────────────────────

export default function ServiceMap() {
  const navigate = useNavigate()
  const [data, setData]           = useState<ServiceMapData | null>(null)
  const [selectedSvc, setSelected] = useState<string | null>(null)
  const [loading, setLoading]     = useState(true)

  const load = useCallback(() => {
    api.serviceMap.get().then(r => {
      setData(r.data ?? { nodes: [], edges: [] })
      setLoading(false)
    })
  }, [])

  useEffect(() => { load() }, [load])

  // re-fetch on new spans
  useWS(ev => {
    if (ev.type === 'span') load()
  })

  const nodes = data?.nodes ?? []
  const edges = data?.edges ?? []

  const nodeIds  = nodes.map(n => n.id)
  const positions = computeLayout(nodeIds, edges)
  const { w, h } = canvasSize(positions)

  const totalSpans  = nodes.reduce((s, n) => s + n.span_count, 0)
  const totalErrors = nodes.reduce((s, n) => s + n.error_count, 0)

  function handleNodeClick(id: string) {
    if (selectedSvc === id) {
      setSelected(null)
    } else {
      setSelected(id)
    }
  }

  function handleGoToTraces() {
    if (selectedSvc) {
      navigate(`/?service=${encodeURIComponent(selectedSvc)}`)
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* header */}
      <div style={{
        padding: '10px 18px',
        borderBottom: '1px solid var(--border)',
        background: 'var(--background)',
        display: 'flex', alignItems: 'center', gap: 14,
        flexShrink: 0,
      }}>
        <div>
          <div style={{
            fontFamily: 'var(--font-sans)', fontSize: 13, fontWeight: 600,
            color: 'var(--foreground)', marginBottom: 2,
          }}>
            Service map
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)' }}>
            auto-generated from span relationships
          </div>
        </div>
        <div style={{ flex: 1 }} />
        {selectedSvc && (
          <button
            type="button"
            onClick={handleGoToTraces}
            style={{
              padding: '5px 12px', borderRadius: 6,
              background: 'var(--muted)',
              border: '1px solid var(--border)',
              color: 'var(--foreground)',
              fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 500,
              cursor: 'pointer', outline: 'none',
            }}
          >
            View traces for {selectedSvc} →
          </button>
        )}
        {/* live badge */}
        <span style={{
          display: 'inline-flex', alignItems: 'center', gap: 6,
          padding: '3px 9px', borderRadius: 14,
          background: 'color-mix(in oklch, #22c55e 18%, var(--background))',
          fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600,
          color: '#22c55e',
        }}>
          <span style={{
            width: 6, height: 6, borderRadius: 6,
            background: '#22c55e', flexShrink: 0,
            boxShadow: '0 0 0 3px #22c55e30',
          }} />
          live
        </span>
      </div>

      {/* canvas */}
      <div style={{
        flex: 1,
        overflow: 'auto',
        position: 'relative',
        background: `
          radial-gradient(circle at 20px 20px, var(--border) 1px, transparent 1px)`,
        backgroundSize: '24px 24px',
      }}>
        {loading ? (
          <div style={{
            position: 'absolute', inset: 0,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: 'var(--muted-foreground)', fontFamily: 'var(--font-mono)', fontSize: 13,
          }}>
            Loading…
          </div>
        ) : nodes.length === 0 ? (
          <Empty />
        ) : (
          <svg
            viewBox={`0 0 ${w} ${h}`}
            width={w} height={h}
            style={{ display: 'block', minWidth: '100%', minHeight: '100%' }}
            onClick={e => { if (e.target === e.currentTarget) setSelected(null) }}
          >
            {/* edges (rendered behind nodes) */}
            {edges.map((e, i) => (
              <SvgEdge
                key={i}
                edge={e}
                pos={positions}
                hot={e.avg_duration_ns > 200_000_000}
              />
            ))}
            {/* nodes */}
            {nodes.map(n => (
              <SvgNode
                key={n.id}
                node={n}
                pos={positions}
                selected={n.id === selectedSvc}
                onClick={() => handleNodeClick(n.id)}
              />
            ))}
          </svg>
        )}
      </div>

      {/* footer legend */}
      <div style={{
        padding: '8px 16px',
        borderTop: '1px solid var(--border)',
        background: 'var(--background)',
        display: 'flex', alignItems: 'center', gap: 18,
        fontFamily: 'var(--font-mono)', fontSize: 10.5,
        color: 'var(--muted-foreground)',
        flexShrink: 0,
      }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <span style={{ width: 14, height: 2, background: '#ef4444', display: 'inline-block' }} />
          edge avg &gt; 200ms
        </span>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <span style={{ width: 14, height: 2, background: 'var(--muted-foreground)', opacity: 0.6, display: 'inline-block' }} />
          normal call
        </span>
        <span style={{ flex: 1 }} />
        {!loading && nodes.length > 0 && (
          <span>
            {nodes.length} services · {edges.length} edges · {totalSpans.toLocaleString()} spans
            {totalErrors > 0 && ` · ${totalErrors} errors`}
          </span>
        )}
      </div>
    </div>
  )
}
