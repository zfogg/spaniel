import { type Dashboard } from '@/lib/api'
export const localKey = 'spaniel.local-dashboards'

export const panelRecipes = [
  {
    type: 'single_value',
    icon: '#',
    label: 'Single value',
    description: 'One current number',
    fields: [
      ['Aggregation', 'count(*)'],
      ['Filter', 'all telemetry'],
    ],
    hint: 'Use one scalar value; alias it as value.',
    shape: [['value', 'number']],
  },
  {
    type: 'time_series',
    icon: '⌁',
    label: 'Time series',
    description: 'Value over time',
    fields: [
      ['Measure', 'count(*)'],
      ['Interval', '1 minute'],
    ],
    hint: 'Return a timestamp and numeric value for each point.',
    shape: [
      ['timestamp', 'timestamp'],
      ['value', 'number'],
    ],
  },
  {
    type: 'table',
    icon: '▤',
    label: 'Table',
    description: 'Ranked records',
    fields: [
      ['Sort by', 'duration descending'],
      ['Limit', '100 rows'],
    ],
    hint: 'Return named columns; preserve a stable order in SQL.',
    shape: [['any named columns', 'record']],
  },
  {
    type: 'heatmap',
    icon: '▦',
    label: 'Heatmap',
    description: 'Distribution',
    fields: [
      ['Bucket', 'duration'],
      ['Aggregate', 'count(*)'],
    ],
    hint: 'Return time, duration buckets, and a count for each cell.',
    shape: [
      ['timestamp_ns', 'time'],
      ['bucket_ms', 'duration'],
      ['value', 'count'],
    ],
  },
  {
    type: 'entity_list',
    icon: '☷',
    label: 'Entity list',
    description: 'Labels, bars, states',
    fields: [
      ['Entity', 'service name'],
      ['Order', 'primary value'],
    ],
    hint: 'Map each row to a readable label and primary value.',
    shape: [
      ['label', 'string'],
      ['primary_value', 'number'],
      ['status', 'optional'],
    ],
  },
  {
    type: 'span_list',
    icon: '⌗',
    label: 'Span list',
    description: 'Inspect spans',
    fields: [
      ['Order', 'newest first'],
      ['Limit', '100 spans'],
    ],
    hint: 'Include trace_id to make each span navigable.',
    shape: [
      ['span_id', 'string'],
      ['trace_id', 'string'],
      ['name', 'string'],
    ],
  },
  {
    type: 'trace_list',
    icon: '◌',
    label: 'Trace list',
    description: 'Investigate requests',
    fields: [
      ['Order', 'newest first'],
      ['Limit', '100 traces'],
    ],
    hint: 'Include trace_id and a human-readable operation name.',
    shape: [
      ['trace_id', 'string'],
      ['service_name', 'string'],
      ['name', 'string'],
    ],
  },
  {
    type: 'log_list',
    icon: '≡',
    label: 'Log list',
    description: 'Read events',
    fields: [
      ['Order', 'newest first'],
      ['Limit', '100 logs'],
    ],
    hint: 'Include timestamp, severity, and body.',
    shape: [
      ['timestamp', 'timestamp'],
      ['severity', 'string'],
      ['body', 'string'],
    ],
  },
  {
    type: 'deploy_correlation',
    icon: '↗',
    label: 'Deploy correlation',
    description: 'Annotate a series',
    fields: [
      ['Measure', 'request latency'],
      ['Annotations', 'release events'],
    ],
    hint: 'Keep a series even when no release source is connected.',
    shape: [
      ['timestamp', 'timestamp'],
      ['value', 'number'],
      ['annotation', 'optional'],
    ],
  },
] as const

export function readLocalDashboards(): Dashboard[] {
  try {
    return JSON.parse(sessionStorage.getItem(localKey) ?? '[]') as Dashboard[]
  } catch {
    return []
  }
}

export function saveLocalDashboards(dashboards: Dashboard[]) {
  sessionStorage.setItem(localKey, JSON.stringify(dashboards))
}

// Vite's SPA fallback returns index.html for /api while the Go server is not
// running. That surfaces as a JSON SyntaxError, which is distinct from a real
// API persistence error and is safe to keep as a local draft.
export function backendUnavailable(error: unknown) {
  return error instanceof TypeError || error instanceof SyntaxError
}

export function nextDashboardName(dashboards: Dashboard[]) {
  const used = dashboards.reduce((highest, dashboard) => {
    const match = /^New dashboard (\d+)$/i.exec(dashboard.name)
    return Math.max(highest, match ? Number(match[1]) : 0)
  }, 0)
  return `New dashboard ${used + 1}`
}
