import type { TraceRow } from './api'

export type TraceTag = 'n+1' | 'slow' | 'baseline' | 'error' | null

const SLOW_NS = 250_000_000

export function traceTag(
  trace: Pick<TraceRow, 'has_n1' | 'issue_kinds' | 'duration_ns' | 'status_code' | 'session_id'>,
  baselineSessionId: string | null,
): TraceTag {
  if (trace.has_n1 || (trace.issue_kinds ?? []).includes('n_plus_one')) return 'n+1'
  if (trace.duration_ns > SLOW_NS) return 'slow'
  if (baselineSessionId && trace.session_id === baselineSessionId) return 'baseline'
  if (trace.status_code === 2) return 'error'
  return null
}
