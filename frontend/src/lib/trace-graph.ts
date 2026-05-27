import type { Span } from '@/lib/api'

const ZERO_ID = '0000000000000000'

export interface GraphNode {
  span: Span
  depth: number
  childIds: string[]
}

export function buildGraph(spans: Span[]): { roots: string[]; nodes: Map<string, GraphNode> } {
  const nodes = new Map<string, GraphNode>()
  const byId = new Map(spans.map(s => [s.span_id, s]))
  for (const s of spans) nodes.set(s.span_id, { span: s, depth: 0, childIds: [] })
  const roots: string[] = []
  for (const s of spans) {
    const pid = s.parent_span_id
    if (!pid || pid === ZERO_ID || !byId.has(pid)) {
      roots.push(s.span_id)
    } else {
      nodes.get(pid)!.childIds.push(s.span_id)
    }
  }
  // depth via BFS from each root
  for (const r of roots) {
    const queue: { id: string; d: number }[] = [{ id: r, d: 0 }]
    while (queue.length) {
      const { id, d } = queue.shift()!
      const n = nodes.get(id)!
      n.depth = d
      for (const c of n.childIds) queue.push({ id: c, d: d + 1 })
    }
  }
  return { roots, nodes }
}

/**
 * Compute the critical path of a trace: from the root with the largest span,
 * walk down picking the child that ends latest at each step. The returned set
 * contains every span_id on that path. For traces with multiple disconnected
 * roots, the deepest-finishing root is chosen.
 */
export function criticalPath(spans: Span[]): Set<string> {
  if (spans.length === 0) return new Set()
  const { roots, nodes } = buildGraph(spans)
  if (roots.length === 0) return new Set()

  // Pick the root whose subtree ends latest.
  const rootEnd = (id: string): number => {
    const n = nodes.get(id)!
    let e = n.span.end_ns
    for (const c of n.childIds) e = Math.max(e, rootEnd(c))
    return e
  }
  let bestRoot = roots[0]
  let bestEnd = rootEnd(bestRoot)
  for (const r of roots.slice(1)) {
    const e = rootEnd(r)
    if (e > bestEnd) { bestEnd = e; bestRoot = r }
  }

  const path = new Set<string>()
  let cur: string | undefined = bestRoot
  while (cur) {
    path.add(cur)
    const n: GraphNode = nodes.get(cur)!
    if (n.childIds.length === 0) break
    // Pick the child that ends latest — that's the one the parent waited on.
    let next: string = n.childIds[0]
    let nextEnd = nodes.get(next)!.span.end_ns
    for (const c of n.childIds.slice(1)) {
      const ce = nodes.get(c)!.span.end_ns
      if (ce > nextEnd) { nextEnd = ce; next = c }
    }
    cur = next
  }
  return path
}
