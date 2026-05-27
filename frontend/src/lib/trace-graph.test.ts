import { describe, it, expect } from 'vitest'
import { buildGraph, criticalPath } from './trace-graph'
import type { Span } from '@/lib/api'

function span(id: string, parent: string, start: number, end: number, name = id): Span {
  return {
    trace_id: 't',
    span_id: id,
    parent_span_id: parent,
    service_name: 'svc',
    name,
    kind: 1,
    start_ns: start,
    end_ns: end,
    duration_ns: end - start,
    status_code: 0,
    status_message: '',
    attributes: '{}',
    resource: '{}',
    session_id: 's',
    session_label: 's',
    received_at: 0,
  } as Span
}

describe('buildGraph', () => {
  it('returns a single root and assigns depths', () => {
    const spans = [
      span('root', '', 0, 100),
      span('a',    'root', 0, 50),
      span('b',    'a',    10, 40),
    ]
    const { roots, nodes } = buildGraph(spans)
    expect(roots).toEqual(['root'])
    expect(nodes.get('root')!.depth).toBe(0)
    expect(nodes.get('a')!.depth).toBe(1)
    expect(nodes.get('b')!.depth).toBe(2)
    expect(nodes.get('root')!.childIds).toEqual(['a'])
    expect(nodes.get('a')!.childIds).toEqual(['b'])
  })

  it('treats spans with the all-zero parent id as roots', () => {
    const spans = [span('root', '0000000000000000', 0, 10)]
    const { roots } = buildGraph(spans)
    expect(roots).toEqual(['root'])
  })

  it('treats spans whose parent is missing as roots (orphans)', () => {
    const spans = [
      span('child', 'missing-parent', 0, 10),
      span('real-root', '', 0, 20),
    ]
    const { roots } = buildGraph(spans)
    expect(roots.sort()).toEqual(['child', 'real-root'])
  })
})

describe('criticalPath', () => {
  it('returns the path through the latest-ending child at each branch', () => {
    // root has 3 parallel children; b ends latest, so the path is root → b.
    const spans = [
      span('root', '', 0, 100),
      span('a',    'root', 0, 30),
      span('b',    'root', 0, 80), // latest end among siblings
      span('c',    'root', 0, 50),
    ]
    const path = criticalPath(spans)
    expect([...path].sort()).toEqual(['b', 'root'])
  })

  it('walks deeply, picking latest end at each step', () => {
    // root → a (ends 60) vs b (ends 90). Pick b. b → b1 (ends 70) vs b2 (ends 90). Pick b2.
    const spans = [
      span('root', '', 0, 100),
      span('a',    'root', 0, 60),
      span('b',    'root', 0, 90),
      span('b1',   'b',    10, 70),
      span('b2',   'b',    10, 90),
    ]
    const path = criticalPath(spans)
    expect([...path].sort()).toEqual(['b', 'b2', 'root'])
  })

  it('returns the single root when there are no children', () => {
    const path = criticalPath([span('only', '', 0, 5)])
    expect([...path]).toEqual(['only'])
  })

  it('returns an empty set for empty input', () => {
    expect(criticalPath([]).size).toBe(0)
  })

  it('picks the root whose subtree ends latest when there are multiple roots', () => {
    const spans = [
      span('r1', '', 0, 20),
      span('r2', '', 0, 10),
      span('r2-child', 'r2', 5, 80), // r2's subtree ends later
    ]
    const path = criticalPath(spans)
    expect(path.has('r2')).toBe(true)
    expect(path.has('r2-child')).toBe(true)
    expect(path.has('r1')).toBe(false)
  })
})
