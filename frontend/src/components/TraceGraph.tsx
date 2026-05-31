import { useMemo, useCallback } from 'react'
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
} from 'reactflow'
import 'reactflow/dist/style.css'
import dagre from '@dagrejs/dagre'

import type { Span } from '@/lib/api'
import { svcColor, fmtNs } from '@/lib/span-utils'
import { buildGraph, criticalPath } from '@/lib/trace-graph'

const NODE_W_MIN = 160
const NODE_W_MAX = 320
const NODE_H = 64

const CRITICAL = 'var(--warn)'

interface SpanNodeData {
  span: Span
  width: number
  selected: boolean
  onCritical: boolean
}

function SpanNode({ data }: NodeProps<SpanNodeData>) {
  const { span, width, selected, onCritical } = data
  const isError = span.status_code === 2
  const svc = svcColor(span.service_name)

  const borderColor =
    onCritical ? CRITICAL
    : isError ? 'var(--danger)'
    : selected ? 'var(--accent)'
    : 'var(--border)'
  const borderWidth = (onCritical || isError || selected) ? 2 : 1

  return (
    <div
      title={`${span.name}\n${span.service_name}\n${fmtNs(span.duration_ns)}`}
      className="flex flex-col justify-between rounded-lg bg-surface px-2.5 py-1.5 cursor-pointer box-border"
      style={{
        width,
        height: NODE_H,
        border: `${borderWidth}px solid ${borderColor}`,
        boxShadow: selected ? '0 0 0 3px var(--accent-soft, rgba(99,102,241,0.18))' : 'none',
      }}
    >
      <div className="truncate font-mono text-xs font-semibold text-foreground">
        {span.name}
      </div>
      <div className="flex items-center justify-between gap-1.5">
        <span
          className="max-w-[60%] truncate rounded-[4px] px-1.5 py-px font-mono text-[10px] font-semibold"
          style={{ color: svc.fg, background: svc.bg }}
        >
          {span.service_name}
        </span>
        <span
          className="font-mono text-[10px] font-semibold"
          style={{ color: onCritical ? CRITICAL : 'var(--muted-foreground)' }}
        >
          {fmtNs(span.duration_ns)}
        </span>
      </div>
    </div>
  )
}

const nodeTypes = { span: SpanNode }

interface Props {
  spans: Span[]
  selectedId: string | null
  onSelect: (spanId: string) => void
}

/**
 * Width-by-duration: scale nodes between NODE_W_MIN..NODE_W_MAX based on each
 * span's duration relative to the longest span in the trace. Gives a visual
 * cue about cost alongside the structural DAG.
 */
function widthFor(durNs: number, maxDurNs: number): number {
  if (maxDurNs <= 0) return NODE_W_MIN
  const ratio = Math.min(1, durNs / maxDurNs)
  return Math.round(NODE_W_MIN + (NODE_W_MAX - NODE_W_MIN) * ratio)
}

export default function TraceGraph({ spans, selectedId, onSelect }: Props) {
  const { nodes, edges } = useMemo(() => layout(spans, selectedId), [spans, selectedId])

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    onSelect(node.id)
  }, [onSelect])

  if (spans.length === 0) {
    return (
      <div className="flex h-full items-center justify-center font-mono text-[13px] text-muted-foreground">
        No spans
      </div>
    )
  }

  return (
    <div className="relative h-full flex-1">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={onNodeClick}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        minZoom={0.1}
        maxZoom={2}
      >
        <Background gap={20} size={1} color="var(--border)" />
        <MiniMap
          pannable
          zoomable
          nodeStrokeWidth={2}
          maskColor="rgba(0,0,0,0.4)"
          style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
        />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function layout(spans: Span[], selectedId: string | null): { nodes: Node[]; edges: Edge[] } {
  const { roots, nodes: graphNodes } = buildGraph(spans)
  const critical = criticalPath(spans)
  const maxDur = spans.reduce((m, s) => Math.max(m, s.duration_ns), 0)

  const g = new dagre.graphlib.Graph()
  g.setDefaultEdgeLabel(() => ({}))
  g.setGraph({ rankdir: 'TB', nodesep: 30, ranksep: 60, marginx: 20, marginy: 20 })

  // Sort children for stable left-to-right ordering by start time.
  for (const gn of graphNodes.values()) {
    gn.childIds.sort((a, b) => {
      const sa = graphNodes.get(a)!.span.start_ns
      const sb = graphNodes.get(b)!.span.start_ns
      return sa - sb
    })
  }

  for (const gn of graphNodes.values()) {
    const w = widthFor(gn.span.duration_ns, maxDur)
    g.setNode(gn.span.span_id, { width: w, height: NODE_H })
  }
  for (const gn of graphNodes.values()) {
    for (const c of gn.childIds) g.setEdge(gn.span.span_id, c)
  }

  dagre.layout(g)

  const rfNodes: Node[] = []
  for (const gn of graphNodes.values()) {
    const w = widthFor(gn.span.duration_ns, maxDur)
    const p = g.node(gn.span.span_id)
    rfNodes.push({
      id: gn.span.span_id,
      type: 'span',
      position: { x: p.x - w / 2, y: p.y - NODE_H / 2 },
      data: {
        span: gn.span,
        width: w,
        selected: gn.span.span_id === selectedId,
        onCritical: critical.has(gn.span.span_id),
      },
      // Disable default node styling — our SpanNode renders the whole chrome.
      style: { background: 'transparent', border: 'none', padding: 0, width: w, height: NODE_H },
    })
  }

  const rfEdges: Edge[] = []
  for (const gn of graphNodes.values()) {
    for (const c of gn.childIds) {
      const child = graphNodes.get(c)!.span
      const gap = child.start_ns - gn.span.end_ns
      // Only show gaps that aren't trivially small; otherwise label clutters.
      const showGap = Math.abs(gap) > 100_000 // 0.1ms
      const onCrit = critical.has(gn.span.span_id) && critical.has(c)
      rfEdges.push({
        id: `${gn.span.span_id}->${c}`,
        source: gn.span.span_id,
        target: c,
        label: showGap ? (gap >= 0 ? `+${fmtNs(gap)}` : fmtNs(gap)) : undefined,
        labelStyle: {
          fontFamily: 'var(--font-mono)', fontSize: 9,
          fill: 'var(--muted-foreground)',
        },
        labelBgStyle: { fill: 'var(--surface)' },
        labelBgPadding: [3, 2],
        labelBgBorderRadius: 3,
        style: {
          stroke: onCrit ? CRITICAL : 'var(--muted-foreground)',
          strokeWidth: onCrit ? 2 : 1,
          opacity: onCrit ? 1 : 0.55,
        },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: onCrit ? CRITICAL : 'var(--muted-foreground)',
          width: 16, height: 16,
        },
        animated: false,
      })
    }
  }

  // Unused locally but ensures roots stay referenced (dagre uses them implicitly).
  void roots
  return { nodes: rfNodes, edges: rfEdges }
}
