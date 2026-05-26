const BASE = ''

async function get<T>(path: string): Promise<{ data: T; meta: { total: number; page: number } }> {
  const res = await fetch(BASE + path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

async function post<T>(path: string, body: unknown): Promise<{ data: T; meta: { total: number } }> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export interface TraceRow {
  trace_id: string
  service_name: string
  name: string
  status_code: number
  start_ns: number
  end_ns: number
  duration_ns: number
  session_id: string
}

export interface Span {
  trace_id: string
  span_id: string
  parent_span_id: string
  service_name: string
  name: string
  kind: number
  start_ns: number
  end_ns: number
  duration_ns: number
  status_code: number
  status_message: string
  attributes: string
  resource: string
  session_id: string
  session_label: string
  received_at: number
}

export interface Log {
  timestamp_ns: number
  trace_id: string
  span_id: string
  severity: number
  body: string
  attributes: string
  service_name: string
  session_id: string
  received_at: number
}

export interface Session {
  id: string
  label: string
  created_at: number
  is_baseline: boolean
  span_count: number
  services: string
}

export interface LintWarning {
  span_id: string
  trace_id: string
  session_id: string
  rule_id: string
  message: string
  severity: string
  created_at: number
}

export interface Stats {
  span_count: number
  trace_count: number
  log_count: number
}

export interface ServiceMapNode {
  id: string
  span_count: number
  error_count: number
}

export interface ServiceMapEdge {
  from: string
  to: string
  call_count: number
  avg_duration_ns: number
}

export interface ServiceMapData {
  nodes: ServiceMapNode[]
  edges: ServiceMapEdge[]
}

export const api = {
  traces: {
    list: (sessionId?: string) =>
      get<TraceRow[]>(`/api/traces${sessionId ? `?sessionId=${sessionId}` : ''}`),
    get: (traceId: string) => get<Span[]>(`/api/traces/${traceId}`),
  },
  spans: {
    get: (spanId: string) => get<Span>(`/api/spans/${spanId}`),
  },
  logs: {
    list: (params?: { sessionId?: string; traceId?: string; spanId?: string }) => {
      const q = new URLSearchParams()
      if (params?.sessionId) q.set('sessionId', params.sessionId)
      if (params?.traceId) q.set('traceId', params.traceId)
      if (params?.spanId) q.set('spanId', params.spanId)
      const qs = q.toString()
      return get<Log[]>(`/api/logs${qs ? `?${qs}` : ''}`)
    },
  },
  services: {
    list: () => get<string[]>('/api/services'),
  },
  sessions: {
    list: () => get<Session[]>('/api/sessions'),
    get: (id: string) => get<Session>(`/api/sessions/${id}`),
    create: (label?: string) => post<Session>('/api/sessions', { label }),
  },
  lint: {
    list: (sessionId?: string) =>
      get<LintWarning[]>(`/api/lint${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  stats: {
    get: (sessionId?: string) =>
      get<Stats>(`/api/stats${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
  serviceMap: {
    get: (sessionId?: string) =>
      get<ServiceMapData>(`/api/service-map${sessionId ? `?sessionId=${sessionId}` : ''}`),
  },
}
