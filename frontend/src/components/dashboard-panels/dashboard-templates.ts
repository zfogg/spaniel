import type { DashboardPanel } from '@/lib/api'

export type TemplatePanel = Pick<DashboardPanel, 'title' | 'display_type' | 'query_sql'>
export type DashboardTemplate = {
  id: string; name: string; icon: string; description: string; needs: string; panels: TemplatePanel[]
}

const hour = (field: string) => `${field} >= cast(epoch(cast(current_timestamp AS TIMESTAMP)) * 1000000000 AS BIGINT) - 3600000000000`
const minute = '(start_ns // 60000000000) * 60000000000'
const panel = (title: string, display_type: TemplatePanel['display_type'], query_sql: string): TemplatePanel => ({ title, display_type, query_sql })
const queue = `(json_extract_string(attributes, '$.messaging.system') IS NOT NULL OR regexp_matches(lower(name), '(^|[ ./_-])(queue|worker|job|consume|process|publish|enqueue)([ ./_-]|$)'))`
const database = `(json_extract_string(attributes, '$.db.system.name') IS NOT NULL OR json_extract_string(attributes, '$.db.system') IS NOT NULL OR regexp_matches(lower(name), '(^|[ ./_-])(sql|query|select|insert|update|delete|database|redis)([ ./_-]|$)'))`
const incoming = `(kind = 2 OR json_extract_string(attributes, '$.http.route') IS NOT NULL)`

// Trace recipes match an interesting span, then inspect the entire trace. Never
// sum overlapping span durations or merge equal trace IDs from different sessions.
function traces(scope: string) {
  return `WITH matched AS (SELECT DISTINCT session_id, trace_id FROM spans WHERE ${scope}),
trace_summary AS (
  SELECT s.session_id, s.trace_id, arg_min(s.name, s.start_ns) AS name,
    arg_min(s.service_name, s.start_ns) AS service_name, min(s.start_ns) AS start_ns,
    max(s.end_ns) - min(s.start_ns) AS duration_ns, count(*) AS span_count
  FROM spans s JOIN matched m ON s.session_id = m.session_id AND s.trace_id = m.trace_id
  GROUP BY s.session_id, s.trace_id
)
SELECT trace_id, name, service_name, start_ns, duration_ns, span_count
FROM trace_summary ORDER BY duration_ns DESC LIMIT 20`
}

function metricTable(pattern: string) {
  // A reported value is not a rate. Preserve service, unit, metric type and
  // attribute sets so histogram quantiles and different hosts stay separate.
  return `SELECT service_name, name, type, unit, attributes,
  arg_max(value, timestamp_ns) AS latest_reported_value
FROM metrics
WHERE ${hour('timestamp_ns')} AND regexp_matches(lower(name), '${pattern}')
GROUP BY service_name, name, type, unit, attributes
ORDER BY service_name, name, attributes LIMIT 30`
}

type Theme = {
  id: string; name: string; icon: string; description: string; needs: string; predicate: string
  titles: [string, string, string, string, string, string, string, string]
  metricPattern?: string; failures?: boolean
}

function themedDashboard(theme: Theme): DashboardTemplate {
  const scope = `${hour('start_ns')} AND ${theme.predicate}`
  const from = `FROM spans WHERE ${scope}`
  const [count, traffic, operations, distribution, entities, slowSpans, slowTraces, logs] = theme.titles
  return { ...theme, panels: [
    panel(count, 'single_value', theme.failures
      ? `SELECT coalesce(100.0 * count(*) FILTER (WHERE status_code = 2) / nullif(count(*), 0), 0) AS value, '%' AS unit\n${from}`
      : `SELECT count(*) AS value\n${from}`),
    panel(traffic, 'time_series', `SELECT ${minute} AS timestamp_ns, service_name AS group_value,
  count(*) AS value
${from}
GROUP BY 1, 2 ORDER BY 1, 2 LIMIT 1000`),
    panel(operations, 'table', theme.metricPattern ? metricTable(theme.metricPattern) : `SELECT service_name, name, count(*) AS calls,
  round(avg(duration_ns / 1000000.0), 2) AS mean_ms,
  round(quantile_cont(duration_ns / 1000000.0, 0.95), 2) AS p95_ms,
  count(*) FILTER (WHERE status_code = 2) AS failed_calls
${from} AND kind = 3
GROUP BY service_name, name ORDER BY p95_ms DESC LIMIT 20`),
    panel(distribution, 'heatmap', `SELECT ${minute} AS timestamp_ns, floor(duration_ns / 10000000.0) * 10 AS bucket_ms, count(*) AS value
${from}
GROUP BY 1, 2 ORDER BY 1, 2 LIMIT 1000`),
    panel(entities, 'entity_list', `SELECT service_name || ' · ' || name AS label,
  count(*) AS primary_value,
  CASE WHEN bool_or(status_code = 2) THEN 'errors observed' ELSE 'no recorded issues' END AS status
${from}
GROUP BY service_name, name ORDER BY primary_value DESC LIMIT 12`),
    panel(slowSpans, 'span_list', `SELECT trace_id, span_id, service_name, name, start_ns, duration_ns, status_code
${from}
ORDER BY duration_ns DESC LIMIT 20`),
    panel(slowTraces, 'trace_list', traces(scope)),
    panel(logs, 'log_list', `SELECT timestamp_ns, severity, body, service_name, trace_id, span_id
FROM logs
WHERE ${hour('timestamp_ns')} AND severity >= 13
  AND EXISTS (SELECT 1 ${from} AND spans.session_id = logs.session_id AND spans.service_name = logs.service_name)
ORDER BY timestamp_ns DESC LIMIT 30`),
  ] }
}

function spanielDashboard(): DashboardTemplate {
  const base = themedDashboard({
    id: 'spaniel', name: 'Spaniel', icon: '◈', predicate: "service_name = 'spaniel'",
    description: 'Watch the observer: ingestion backlog, DuckDB footprint, internal calls, runtime measurements, and errors.',
    needs: 'Enable self-monitoring in Settings. Uses service.name = spaniel and Spaniel’s owned metrics; empty panels mean that signal has not arrived. Last hour.',
    titles: ['DuckDB footprint · MiB', 'Ingestion queue depth · reported requests', 'Ingestion and storage failure counters · latest reported', 'Self-monitor span duration · 10 ms buckets', 'Spaniel’s busiest internal operations', 'Slow internal spans', 'Slow self-monitoring traces', 'Recent Spaniel log records'],
  })
  const metrics = `FROM metrics WHERE ${hour('timestamp_ns')} AND service_name = 'spaniel'`
  base.panels[0].query_sql = `SELECT arg_max(value, timestamp_ns) / 1048576.0 AS value ${metrics} AND name = 'spaniel.storage.db_size.current'`
  base.panels[1].query_sql = `SELECT (timestamp_ns // 60000000000) * 60000000000 AS timestamp_ns,
  attributes AS group_value, arg_max(value, timestamp_ns) AS value
${metrics} AND name = 'spaniel.ingest.queue.depth'
GROUP BY 1, 2 ORDER BY 1, 2 LIMIT 1000`
  base.panels[2].query_sql = `SELECT name, unit, attributes, arg_max(value, timestamp_ns) AS latest_reported_count
${metrics} AND name IN ('spaniel.ingest.dropped', 'spaniel.ingest.rejected', 'spaniel.ingest.decode.errors', 'spaniel.ingest.rate_limited', 'spaniel.ingest.storage_full_rejections', 'spaniel.storage.failures', 'spaniel.storage.flush.errors', 'spaniel.forwarder.dropped')
GROUP BY name, unit, attributes ORDER BY name, attributes LIMIT 40`
  base.panels[7].query_sql = `SELECT timestamp_ns, severity, body, service_name, trace_id, span_id
FROM logs WHERE ${hour('timestamp_ns')} AND service_name = 'spaniel'
ORDER BY timestamp_ns DESC LIMIT 30`
  base.panels.push(
    panel('Spaniel uptime · seconds', 'single_value', `SELECT arg_max(value, timestamp_ns) AS value ${metrics} AND name = 'spaniel.server.uptime'`),
    panel('Runtime and live storage gauges · latest reported', 'table', `SELECT name, unit, attributes, arg_max(value, timestamp_ns) AS latest_reported_value
${metrics} AND (regexp_matches(name, '^(go|process|runtime)[.]') OR name IN ('spaniel.storage.db_size.limit', 'spaniel.storage.full', 'spaniel.storage.write_queue.depth', 'spaniel.ingest.queue.capacity', 'spaniel.websocket.clients', 'spaniel.metrics.series.count'))
GROUP BY name, unit, attributes ORDER BY name, attributes LIMIT 40`),
  )
  return base
}

export const dashboardTemplates: DashboardTemplate[] = [
  { id: 'none', name: 'None', icon: '⊞', description: 'An empty dashboard. Add only the panels you need.', needs: '', panels: [] },
  spanielDashboard(),
  themedDashboard({
    id: 'service', name: 'Service overview', icon: '◫', predicate: incoming,
    description: 'The team’s morning check: incoming traffic, tail latency, noisy services, and warnings.',
    needs: 'HTTP/server spans (for example GET /checkout), logs, and process/runtime metrics. Last hour, all services.',
    metricPattern: '(^process[.]|^system[.]|^runtime[.]|^go[.])',
    titles: ['Incoming requests · last hour', 'Request volume by service · per minute', 'Runtime pulse · latest reported metrics', 'Request duration · 10 ms buckets', 'Busiest endpoints · request count', 'Slowest incoming spans', 'Slowest request journeys', 'Warnings from request-serving services'],
  }),
  themedDashboard({
    id: 'requests', name: 'Request path', icon: '◌', predicate: 'TRUE', failures: true,
    description: 'Follow a slow checkout from the edge to the database. Find costly calls and the logs that explain them.',
    needs: 'Distributed traces and client spans (HTTP, RPC or database), with correlated logs. Last hour, all operations.',
    titles: ['Failed spans · percent', 'Work across services · spans per minute', 'Downstream calls ranked by p95 · ms', 'Span duration · 10 ms buckets', 'Most-called operations · span count', 'Expensive individual spans', 'Longest end-to-end traces', 'Warnings along the request path'],
  }),
  themedDashboard({
    id: 'queue', name: 'Queue worker', icon: '⇄', predicate: queue,
    description: 'Keep background work moving: processing volume, slow jobs, retries, and queue measurements.',
    needs: 'messaging.system or span names such as jobs/process, queue/publish or worker/send-email; queue metrics and worker logs. Last hour.',
    metricPattern: '(queue|messaging|worker|job|consumer|retry|dead.?letter)',
    titles: ['Messaging operations · last hour', 'Worker activity · operations per minute', 'Queue and retry metrics · latest reported', 'Job duration · 10 ms buckets', 'Busiest worker operations', 'Longest-running worker spans', 'Slow traces involving background work', 'Worker warnings and failed-job logs'],
  }),
  themedDashboard({
    id: 'database', name: 'Database health', icon: '▤', predicate: database, failures: true,
    description: 'A database on-call desk: failed queries, query volume, slow statements, and pool pressure.',
    needs: 'db.system.name/db.system attributes or SQL/query/redis span names; database/pool metrics and application logs. Last hour.',
    metricPattern: '(^db[.]|database|postgres|mysql|redis|connection.*pool|pool.*connection)',
    titles: ['Failed database calls · percent', 'Database call volume · per minute', 'Database and connection-pool metrics · latest', 'Database duration · 10 ms buckets', 'Busiest database operations', 'Slow database spans', 'Slow requests that touch the database', 'Warnings from database-calling services'],
  }),
]
