import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ServiceMapData, ServiceMapNode, ServiceMapEdge, Session, SourceStats } from '@/lib/api'
import { useWS } from '@/lib/ws'
import EmptyState from '@/components/EmptyState'

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
const GAP_X  = 168 // wide enough to fit the "N calls · Xms avg" edge label between layers
const GAP_Y  = 34
const PAD    = 40
const LANE   = 9  // vertical offset for one half of a bidirectional pair

function computeLayout(
  nodeIds: string[],
  edges: { from: string; to: string }[],
): Map<string, { x: number; y: number }> {
  const outgoing = new Map<string, string[]>()
  const incoming = new Map<string, string[]>()
  const inDegree  = new Map<string, number>()
  for (const n of nodeIds) { outgoing.set(n, []); incoming.set(n, []); inDegree.set(n, 0) }
  for (const e of edges) {
    if (e.from === e.to) continue
    outgoing.get(e.from)?.push(e.to)
    incoming.get(e.to)?.push(e.from)
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

  // group into ordered layers (sorted by layer index)
  const maxLayer = Math.max(0, ...[...layer.values()])
  const layers: string[][] = Array.from({ length: maxLayer + 1 }, () => [])
  for (const [n, l] of layer) layers[l].push(n)

  // ── crossing reduction (barycenter / Sugiyama) ──────────────────────────────
  // Repeatedly reorder each layer so a node sits near the average position of its
  // neighbours in the adjacent layer; this is what stops edges from overlapping.
  const order = new Map<string, number>()
  layers.forEach(l => l.forEach((n, i) => order.set(n, i)))

  const barycenter = (n: string, neighbors: Map<string, string[]>): number => {
    const idx = (neighbors.get(n) ?? []).map(m => order.get(m)).filter((v): v is number => v != null)
    return idx.length ? idx.reduce((a, b) => a + b, 0) / idx.length : (order.get(n) ?? 0)
  }

  for (let sweep = 0; sweep < 12; sweep++) {
    const downward = sweep % 2 === 0
    const indices = downward
      ? layers.map((_, i) => i)
      : layers.map((_, i) => i).reverse()
    for (const li of indices) {
      const neighbors = downward ? incoming : outgoing
      // layer 0 (downward) / last layer (upward) has no reference neighbours
      if ((downward && li === 0) || (!downward && li === layers.length - 1)) continue
      const ranked = layers[li]
        .map(n => ({ n, b: barycenter(n, neighbors) }))
        .sort((a, b) => a.b - b.b)
      layers[li] = ranked.map(r => r.n)
      layers[li].forEach((n, i) => order.set(n, i))
    }
  }

  const positions = new Map<string, { x: number; y: number }>()
  layers.forEach((nodes, l) => {
    const x = PAD + l * (NODE_W + GAP_X)
    nodes.forEach((n, i) => {
      positions.set(n, { x, y: PAD + i * (NODE_H + GAP_Y) })
    })
  })
  return positions
}

// Endpoints + bezier midpoint for an edge. Connects the facing sides of the two
// nodes (right→left going forward, left→right going back) and applies a vertical
// `lane` offset so the two halves of a bidirectional pair don't overlap.
function edgeGeom(
  A: { x: number; y: number },
  B: { x: number; y: number },
  srcOff = 0,
  dstOff = 0,
) {
  const ltr = B.x >= A.x
  const x1 = ltr ? A.x + NODE_W : A.x
  const x2 = ltr ? B.x : B.x + NODE_W
  const y1 = A.y + NODE_H / 2 + srcOff
  const y2 = B.y + NODE_H / 2 + dstOff
  return { x1, y1, x2, y2, mx: (x1 + x2) / 2, dir: ltr ? 1 : -1 }
}

type EdgePorts = { src: number[]; dst: number[] }

// Spread the edges that share a node face across that face instead of letting
// them all leave/enter at the node centre. Edges on a face are ordered by the
// vertical position of their opposite endpoint so the fan-out doesn't cross
// itself. This stops two same-direction edges from lying on top of each other,
// and also separates the two halves of a bidirectional pair.
function computePorts(
  edges: ServiceMapEdge[],
  pos: Map<string, { x: number; y: number }>,
): EdgePorts {
  type Slot = { i: number; role: 'src' | 'dst'; otherY: number }
  const buckets = new Map<string, Slot[]>()
  const add = (k: string, s: Slot) => {
    const b = buckets.get(k)
    if (b) b.push(s); else buckets.set(k, [s])
  }

  edges.forEach((e, i) => {
    const A = pos.get(e.from), B = pos.get(e.to)
    if (!A || !B) return
    const ltr = B.x >= A.x
    add(`${e.from}|${ltr ? 'R' : 'L'}`, { i, role: 'src', otherY: B.y })
    add(`${e.to}|${ltr ? 'L' : 'R'}`,   { i, role: 'dst', otherY: A.y })
  })

  const src = new Array(edges.length).fill(0)
  const dst = new Array(edges.length).fill(0)
  for (const slots of buckets.values()) {
    if (slots.length < 2) continue
    slots.sort((a, b) => a.otherY - b.otherY)
    const span = Math.min(NODE_H - 14, (slots.length - 1) * (LANE + 2))
    slots.forEach((s, k) => {
      const off = -span / 2 + (span / (slots.length - 1)) * k
      if (s.role === 'src') src[s.i] = off
      else dst[s.i] = off
    })
  }
  return { src, dst }
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

// ── edge label placement ───────────────────────────────────────────────────────

type EdgeLabel = { x: number; y: number; w: number; text: string }

// Place each edge label in the empty inter-layer gap just past its source node
// (so it never sits under an intermediate node, which renders above edges), then
// push apart labels that share a gap so they don't stack on top of each other.
function computeEdgeLabels(
  edges: ServiceMapEdge[],
  pos: Map<string, { x: number; y: number }>,
  ports: EdgePorts,
): Map<number, EdgeLabel> {
  const ROW = 14
  const list: (EdgeLabel & { i: number })[] = []

  edges.forEach((edge, i) => {
    const A = pos.get(edge.from), B = pos.get(edge.to)
    if (!A || !B) return
    const { x1, y1, x2, y2, dir } = edgeGeom(A, B, ports.src[i], ports.dst[i])
    const x = x1 + dir * (GAP_X / 2)
    const t = x2 !== x1 ? Math.min(0.5, (GAP_X / 2) / Math.abs(x2 - x1)) : 0.5
    const y = y1 + (y2 - y1) * t - 4
    const text =
      `${dir === 1 ? '→' : '←'} ${edge.call_count} calls · ${fmtNs(edge.avg_duration_ns)} avg` +
      (edge.error_count > 0 ? ` · ${edge.error_count} err` : '')
    list.push({ i, x, y, w: text.length * 5.0 + 10, text })
  })

  // declutter vertically within each gap column
  const groups = new Map<number, (EdgeLabel & { i: number })[]>()
  for (const l of list) {
    const key = Math.round(l.x)
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key)!.push(l)
  }
  for (const g of groups.values()) {
    g.sort((a, b) => a.y - b.y)
    for (let k = 1; k < g.length; k++) {
      if (g[k].y - g[k - 1].y < ROW) g[k].y = g[k - 1].y + ROW
    }
  }

  const out = new Map<number, EdgeLabel>()
  for (const l of list) out.set(l.i, { x: l.x, y: l.y, w: l.w, text: l.text })
  return out
}

// ── SVG pieces ────────────────────────────────────────────────────────────────

function SvgEdge({ edge, pos, label, srcOff, dstOff, hot, dim, onHover }: {
  edge: ServiceMapEdge
  pos: Map<string, { x: number; y: number }>
  label?: EdgeLabel
  srcOff: number
  dstOff: number
  hot: boolean
  dim: boolean
  onHover: (e: ServiceMapEdge | null) => void
}) {
  const A = pos.get(edge.from), B = pos.get(edge.to)
  if (!A || !B) return null

  const { x1, y1, x2, y2, mx } = edgeGeom(A, B, srcOff, dstOff)
  const hasError = edge.error_count > 0
  const stroke = hasError ? '#ef4444' : hot ? '#f59e0b' : 'var(--muted-foreground)'
  const sw = Math.min(4, 0.8 + edge.call_count * 0.6)
  const baseOpacity = hasError ? 0.9 : hot ? 0.8 : 0.5
  const opacity = dim ? 0.2 : baseOpacity
  const markerId = `arrow-${hasError ? 'err' : hot ? 'hot' : 'norm'}`

  return (
    <g
      data-testid={`edge-${edge.from}-to-${edge.to}`}
      onMouseEnter={() => onHover(edge)}
      onMouseLeave={() => onHover(null)}
      className="cursor-pointer"
    >
      <defs>
        <marker
          id={markerId}
          markerWidth="6" markerHeight="6"
          refX="5" refY="3"
          orient="auto"
        >
          <path d="M0,0 L0,6 L6,3 z" fill={stroke} opacity={baseOpacity} />
        </marker>
      </defs>
      {/* invisible wide hit target so hover works on the curve */}
      <path
        d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
        fill="none"
        stroke="transparent"
        strokeWidth={Math.max(12, sw * 4)}
      />
      <path
        d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
        fill="none"
        stroke={stroke}
        strokeWidth={sw}
        opacity={opacity}
        markerEnd={`url(#${markerId})`}
      />
      {label && (
        <>
          <rect
            x={label.x - label.w / 2} y={label.y - 9}
            width={label.w} height={13}
            rx="3" ry="3"
            fill="var(--background)"
            opacity={dim ? 0.4 : 0.92}
            pointerEvents="none"
          />
          <text
            x={label.x} y={label.y}
            textAnchor="middle"
            fontFamily="var(--font-mono)" fontSize="9"
            fill={hasError ? '#ef4444' : 'var(--muted-foreground)'}
            opacity={dim ? 0.4 : 1}
            pointerEvents="none"
          >
            {label.text}
          </text>
        </>
      )}
    </g>
  )
}

function SvgNode({ node, pos, selected, highlighted, dim, onClick }: {
  node: ServiceMapNode
  pos: Map<string, { x: number; y: number }>
  selected: boolean
  highlighted: boolean
  dim: boolean
  onClick: () => void
}) {
  const p = pos.get(node.id)
  if (!p) return null
  const c = svcColor(node.id)
  const hasError = node.error_count > 0
  const errRate = node.span_count > 0 ? node.error_count / node.span_count : 0
  const borderColor = hasError ? '#ef4444' : c.fg
  const borderWidth = selected ? 2.5 : hasError ? 2 : highlighted ? 2 : 1.5
  const opacity = dim ? 0.35 : 1

  return (
    <g
      data-testid={`node-${node.id}`}
      data-error={hasError ? 'true' : 'false'}
      transform={`translate(${p.x}, ${p.y})`}
      onClick={onClick}
      className="cursor-pointer"
      style={{ opacity }}
    >
      <rect
        width={NODE_W} height={NODE_H}
        rx="10" ry="10"
        fill={c.bg}
        stroke={borderColor}
        strokeOpacity={selected || hasError || highlighted ? 0.9 : 0.35}
        strokeWidth={borderWidth}
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

// ── Node inspector ────────────────────────────────────────────────────────────

function NodeInspector({ node, onClose }: { node: ServiceMapNode; onClose: () => void }) {
  const errRate = node.span_count > 0 ? (node.error_count / node.span_count) * 100 : 0
  const c = svcColor(node.id)
  const hasError = node.error_count > 0

  return (
    <aside
      data-testid="node-inspector"
      className="flex w-[320px] shrink-0 flex-col overflow-y-auto border-l border-border bg-background"
    >
      <div className="flex items-start gap-3 border-b border-border p-4">
        <span
          className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 font-mono text-[12px] font-semibold"
          style={{ background: c.bg, color: c.fg }}
        >
          <span className="size-2 rounded-full" style={{ background: c.fg }} />
          {node.id}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          onClick={onClose}
          aria-label="close inspector"
          className="cursor-pointer rounded-md border border-border bg-muted px-2 py-0.5 font-mono text-[11px] text-muted-foreground outline-hidden hover:text-foreground"
        >
          ×
        </button>
      </div>

      <div className="grid grid-cols-2 border-b border-border">
        <Stat label="spans" value={node.span_count.toLocaleString()} />
        <Stat label="p95" value={fmtNs(node.p95_ns)} />
        <Stat label="errors" value={String(node.error_count)} tone={hasError ? 'danger' : undefined} />
        <Stat label="err rate" value={`${errRate.toFixed(1)}%`} tone={errRate > 5 ? 'danger' : undefined} />
      </div>

      <div className="p-4">
        <div className="mb-2 font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">
          top operations
        </div>
        {(node.top_operations ?? []).length === 0 ? (
          <div className="font-mono text-[11px] text-muted-foreground">no operations recorded</div>
        ) : (
          <ul className="space-y-1.5">
            {(node.top_operations ?? []).map(op => (
              <li
                key={op.name}
                className="flex items-baseline justify-between gap-2 rounded-md border border-border bg-muted/40 px-2.5 py-1.5"
              >
                <span className="min-w-0 truncate font-mono text-[11px] text-foreground">{op.name}</span>
                <span className="font-mono text-[10px] text-muted-foreground tabular-nums">
                  {op.count}× · {fmtNs(op.p95_ns)} p95
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  )
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return (
    <div className="border-r border-b border-border p-3 last:border-r-0">
      <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-muted-foreground">{label}</div>
      <div
        className={
          'mt-1 font-serif text-[22px] font-semibold leading-none ' +
          (tone === 'danger' ? 'text-[#b04040]' : 'text-foreground')
        }
      >
        {value}
      </div>
    </div>
  )
}

// ── Empty state ───────────────────────────────────────────────────────────────

function Empty() {
  return (
    <div className="absolute inset-0">
      <EmptyState
        title="No service relationships yet"
        hint="Once two services exchange spans, the map fills in automatically."
        glyph={
          <svg width="36" height="36" viewBox="0 0 48 48" fill="none">
            <circle cx="16" cy="24" r="7" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
            <circle cx="36" cy="12" r="5" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
            <circle cx="36" cy="36" r="5" stroke="currentColor" strokeWidth="1.5" opacity="0.5" />
            <line x1="23" y1="20" x2="31" y2="14" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
            <line x1="23" y1="28" x2="31" y2="34" stroke="currentColor" strokeWidth="1.5" opacity="0.4" />
          </svg>
        }
      />
    </div>
  )
}

// ── ServiceMap page ───────────────────────────────────────────────────────────

function fmtSourceRate(n: number): string {
  if (n <= 0) return '0'
  if (n < 1) return '<1'
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return n.toFixed(1)
}

function fmtSourceBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

export default function ServiceMap() {
  const navigate = useNavigate()
  const [data, setData]            = useState<ServiceMapData | null>(null)
  const [selectedSvc, setSelected] = useState<string | null>(null)
  const [hoverEdge, setHoverEdge]  = useState<ServiceMapEdge | null>(null)
  const [loading, setLoading]      = useState(true)
  const [sessions, setSessions]    = useState<Session[]>([])
  const [sessionId, setSessionId]  = useState<string>('') // '' = all
  const [tab, setTab]              = useState<'map' | 'sources'>('map')
  const [sources, setSources]      = useState<SourceStats[]>([])
  const [sourceSort, setSourceSort] = useState<keyof SourceStats>('accepted_per_sec')

  const load = useCallback((sid: string) => {
    api.serviceMap.get(sid || undefined).then(r => {
      setData(r.data ?? { nodes: [], edges: [] })
      setLoading(false)
    })
  }, [])

  useEffect(() => { load(sessionId) }, [load, sessionId])

  useEffect(() => {
    api.sessions.list().then(r => setSessions(r.data ?? [])).catch(() => {})
  }, [])

  // re-fetch on new spans
  useWS(ev => {
    if (ev.type === 'span') load(sessionId)
  })

  // poll /api/sources every 2s
  useEffect(() => {
    let cancel = false
    function refresh() {
      api.sources.list().then(r => { if (!cancel) setSources(r.data ?? []) }).catch(() => {})
    }
    refresh()
    const t = setInterval(refresh, 2000)
    return () => { cancel = true; clearInterval(t) }
  }, [])

  const nodes = data?.nodes ?? []
  const edges = data?.edges ?? []

  const nodeIds  = nodes.map(n => n.id)
  const positions = computeLayout(nodeIds, edges)
  const ports = computePorts(edges, positions)
  const edgeLabels = computeEdgeLabels(edges, positions, ports)
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

  const selectedNode = selectedSvc ? nodes.find(n => n.id === selectedSvc) ?? null : null

  const sortedSources = [...sources]
    .sort((a, b) => (b[sourceSort] as number) - (a[sourceSort] as number))
    .slice(0, 10)

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* header */}
      <header className="flex shrink-0 items-center gap-3.5 border-b border-border bg-background px-[18px] py-2.5">
        <div>
          <div className="font-sans text-[13px] font-semibold text-foreground">Services</div>
          <div className="mt-0.5 font-mono text-[10px] text-muted-foreground">
            {tab === 'map' ? 'auto-generated from span relationships' : 'live ingest rates per service'}
          </div>
        </div>

        {/* tab toggle */}
        <div className="flex rounded-md border border-border overflow-hidden font-mono text-[11px]">
          <button
            data-testid="tab-map"
            onClick={() => setTab('map')}
            className={`px-3 py-1 ${tab === 'map' ? 'bg-surface text-ink' : 'bg-background text-ink3 hover:text-ink'}`}
          >
            Map
          </button>
          <button
            data-testid="tab-sources"
            onClick={() => setTab('sources')}
            className={`px-3 py-1 border-l border-border ${tab === 'sources' ? 'bg-surface text-ink' : 'bg-background text-ink3 hover:text-ink'}`}
          >
            Sources
            {sources.some(s => s.rejected_per_sec > 0) && (
              <span className="ml-1 inline-block w-1.5 h-1.5 rounded-full bg-danger align-middle" aria-label="rate-limited sources" />
            )}
          </button>
        </div>

        <div className="flex-1" />

        {/* session filter */}
        <label className="inline-flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
          session
          <select
            data-testid="session-filter"
            value={sessionId}
            onChange={e => setSessionId(e.target.value)}
            className="rounded-md border border-border bg-background px-2 py-1 font-mono text-[11px] text-foreground outline-hidden"
          >
            <option value="">all sessions</option>
            {sessions.map(s => (
              <option key={s.id} value={s.id}>{s.label || s.id.slice(0, 12)}</option>
            ))}
          </select>
        </label>

        {selectedSvc && (
          <button
            type="button"
            onClick={handleGoToTraces}
            className="cursor-pointer rounded-md border border-border bg-muted px-3 py-1 font-mono text-[11px] font-medium text-foreground outline-hidden hover:bg-muted/80"
          >
            View traces for {selectedSvc} →
          </button>
        )}

        {/* live badge */}
        <span className="inline-flex items-center gap-1.5 rounded-full bg-[color-mix(in_oklch,#22c55e_18%,var(--background))] px-2 py-0.5 font-mono text-[10px] font-semibold text-[#22c55e]">
          <span className="size-1.5 shrink-0 rounded-full bg-[#22c55e] shadow-[0_0_0_3px_#22c55e30]" />
          live
        </span>
      </header>

      {/* sources table */}
      {tab === 'sources' && (
        <div className="flex-1 overflow-auto p-4">
          {sortedSources.length === 0 ? (
            <p className="font-mono text-[11px] text-ink3 mt-8 text-center">No sources seen yet — start sending spans.</p>
          ) : (
            <table className="w-full font-mono text-[11px] border border-border rounded-lg overflow-hidden" data-testid="sources-table">
              <thead>
                <tr className="bg-surface-raised border-b border-border text-ink3">
                  <th className="px-3 py-2 text-left">service</th>
                  {([
                    ['accepted/s', 'accepted_per_sec'],
                    ['rejected/s', 'rejected_per_sec'],
                    ['errors',   'error_rate'],
                    ['bytes/s',  'bytes_per_sec'],
                  ] as [string, keyof SourceStats][]).map(([label, k]) => (
                    <th
                      key={k}
                      className={`px-3 py-2 text-right cursor-pointer select-none hover:text-ink ${sourceSort === k ? 'text-ink' : ''}`}
                      onClick={() => setSourceSort(k)}
                    >
                      {label}{sourceSort === k ? ' ↓' : ''}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sortedSources.map(s => {
                  const rateLimited = s.rejected_per_sec > 0
                  return (
                    <tr key={s.service} className="border-b border-border/50 hover:bg-surface-raised/40" data-testid="source-row">
                      <td className="px-3 py-2 text-ink flex items-center gap-1.5">
                        {rateLimited && (
                          <span
                            className="shrink-0 inline-block w-1.5 h-1.5 rounded-full bg-danger"
                            aria-label="rate-limited"
                            data-testid="rate-limited-badge"
                          />
                        )}
                        <span className="truncate max-w-[220px]">{s.service}</span>
                      </td>
                      <td className="px-3 py-2 text-right text-ok">{fmtSourceRate(s.accepted_per_sec)}</td>
                      <td className={`px-3 py-2 text-right ${rateLimited ? 'text-danger font-semibold' : 'text-ink3'}`}>
                        {fmtSourceRate(s.rejected_per_sec)}
                      </td>
                      <td className={`px-3 py-2 text-right ${s.error_rate > 0.05 ? 'text-warning' : 'text-ink3'}`}>
                        {(s.error_rate * 100).toFixed(1)}%
                      </td>
                      <td className="px-3 py-2 text-right text-ink2">{fmtSourceBytes(s.bytes_per_sec)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* canvas + inspector */}
      {tab === 'map' && <div className="flex min-h-0 flex-1">
        <div
          className="relative flex-1 overflow-auto bg-background"
          style={{
            backgroundImage: 'radial-gradient(circle at 20px 20px, var(--border) 1px, transparent 1px)',
            backgroundSize: '24px 24px',
          }}
        >
          {loading ? (
            <div className="absolute inset-0 flex items-center justify-center font-mono text-[13px] text-muted-foreground">
              Loading…
            </div>
          ) : nodes.length === 0 ? (
            <Empty />
          ) : (
            <svg
              viewBox={`0 0 ${w} ${h}`}
              width={w} height={h}
              className="block min-h-full min-w-full"
              onClick={e => { if (e.target === e.currentTarget) { setSelected(null); setHoverEdge(null) } }}
            >
              {/* edges (rendered behind nodes) */}
              {edges.map((e, i) => {
                const isHoveredEdge = hoverEdge?.from === e.from && hoverEdge?.to === e.to
                const dim = hoverEdge != null && !isHoveredEdge
                return (
                  <SvgEdge
                    key={i}
                    edge={e}
                    pos={positions}
                    label={edgeLabels.get(i)}
                    srcOff={ports.src[i]}
                    dstOff={ports.dst[i]}
                    hot={e.avg_duration_ns > 200_000_000}
                    dim={dim}
                    onHover={setHoverEdge}
                  />
                )
              })}
              {/* nodes */}
              {nodes.map(n => {
                const highlighted = hoverEdge != null && (hoverEdge.from === n.id || hoverEdge.to === n.id)
                const dim = hoverEdge != null && !highlighted
                return (
                  <SvgNode
                    key={n.id}
                    node={n}
                    pos={positions}
                    selected={n.id === selectedSvc}
                    highlighted={highlighted}
                    dim={dim}
                    onClick={() => handleNodeClick(n.id)}
                  />
                )
              })}
            </svg>
          )}
        </div>

        {selectedNode && (
          <NodeInspector node={selectedNode} onClose={() => setSelected(null)} />
        )}
      </div>}

      {/* footer legend */}
      <footer className="flex shrink-0 items-center gap-[18px] border-t border-border bg-background px-4 py-2 font-mono text-[10.5px] text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-3.5 bg-[#ef4444]" />
          edge has errors
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-3.5 bg-[#f59e0b]" />
          avg &gt; 200ms
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-0.5 w-3.5 bg-muted-foreground/60" />
          normal call
        </span>
        <span className="flex-1" />
        {!loading && nodes.length > 0 && (
          <span>
            {nodes.length} services · {edges.length} edges · {totalSpans.toLocaleString()} spans
            {totalErrors > 0 && ` · ${totalErrors} errors`}
          </span>
        )}
      </footer>
    </div>
  )
}
