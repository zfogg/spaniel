export type Source = 'spans' | 'traces' | 'logs' | 'metrics'
// Aggregate before filtering: duration and status include every stored span.
export const traceRelation = (includeAttributes = false) => `(SELECT session_id, trace_id,
  arg_min(name, start_ns) AS name, arg_min(service_name, start_ns) AS service_name,
  min(start_ns) AS start_ns, max(end_ns) - min(start_ns) AS duration_ns,
  count(*) AS span_count,
  CASE WHEN bool_or(status_code = 2) THEN 2 WHEN bool_or(status_code = 1) THEN 1 ELSE 0 END AS status_code,
  ${includeAttributes ? "string_agg(coalesce(attributes, ''), ' ')" : "''"} AS attributes
FROM spans WHERE trace_id IS NOT NULL AND trace_id <> ''
GROUP BY session_id, trace_id) AS traces`
// Fixed durations shared by the UI and SQL allowlist; months/years are not calendar buckets.
export const intervalOptions: string[][] = [
  ['1', '1 second'], ['5', '5 seconds'], ['10', '10 seconds'], ['30', '30 seconds'],
  ['60', '1 minute'], ['300', '5 minutes'], ['600', '10 minutes'], ['900', '15 minutes'], ['1800', '30 minutes'],
  ['3600', '1 hour'], ['7200', '2 hours'], ['10800', '3 hours'], ['21600', '6 hours'], ['43200', '12 hours'],
  ['86400', '1 day'], ['172800', '2 days'], ['604800', '1 week'], ['1209600', '2 weeks'],
  ['2592000', '1 month (30 days)'], ['7776000', '3 months (90 days)'], ['15552000', '6 months (180 days)'],
  ['31536000', '1 year (365 days)'], ['63072000', '2 years (730 days)'], ['94608000', '3 years (1,095 days)'],
]
export const timeRangeOptions: string[][] = [...intervalOptions.map(([value, label]) => [value, `Last ${label}`]), ['0', 'All collected data']]
export type BuilderOptions = {
  source: Source; measure: string; interval: string; group: string; service: string
  operation: string; metric: string; percentile: string; filter: string; text: string
  order: string; limit: string; bucket: string; window: string
}
export const builderDefaults: BuilderOptions = { source: 'spans', measure: 'count', interval: '60', group: '', service: '', operation: '', metric: '', percentile: 'p95', filter: '', text: '', order: 'newest', limit: '100', bucket: '10', window: '3600' }
export const measureOptions = (source: Source) => source === 'metrics'
  ? [['latest', 'Latest reported value'], ['avg', 'Mean reported value'], ['min', 'Minimum reported value'], ['max', 'Maximum reported value'], ['count', 'Sample count']]
  : source === 'logs' ? [['count', 'Log count'], ['errors', 'Error log count'], ['error_rate', 'Error log percentage']]
    : [['count', source === 'traces' ? 'Trace count' : 'Span count'], ['success', source === 'traces' ? 'Traces with OK status and no errors' : 'Successful span count (explicit OK)'], ['non_error', 'Non-error count (OK or unset)'], ['unset', 'Unset status count'], ['success_rate', 'Explicit OK percentage'], ['errors', source === 'traces' ? 'Failed trace count' : 'Failed span count'], ['error_rate', 'Failed percentage'], ['min', 'Shortest duration (ms)'], ['avg', 'Mean duration (ms)'], ['p50', 'Median duration (ms)'], ['p95', 'p95 duration (ms)'], ['p99', 'p99 duration (ms)'], ['max', 'Longest duration (ms)'], ['sum', 'Total duration (ms)'], ...(source === 'traces' ? [['span_count', 'Total spans in traces'], ['avg_spans', 'Mean spans per trace'], ['max_spans', 'Most spans per trace']] : [])]
export const groupOptions = (source: Source) => [['', 'None'], ['service_name', 'Service'], ...(source === 'logs' ? [['severity', 'Severity']] : source === 'metrics' ? [['attributes', 'Attribute set'], ['name', 'Metric name']] : [['name', 'Operation'], ['status_code', source === 'traces' ? 'Trace status' : 'Span status'], ...(source === 'traces' ? [] : [['kind', 'Span kind']])])]
const literal = (value: string) => `'${value.replace(/'/g, "''")}'`
const bounded = (value: string, allowed: string[], fallback: string) => allowed.includes(value) ? value : fallback

export function buildPanelSQL(type: string, options: BuilderOptions): string {
  const source: Source = type === 'log_list' ? 'logs' : type === 'trace_list' ? 'traces' : type === 'span_list' ? 'spans' : options.source
  const isSpanOrTrace = source === 'spans' || source === 'traces'
  const time = isSpanOrTrace ? 'start_ns' : 'timestamp_ns'
  const interval = Number(bounded(options.interval, intervalOptions.map(([value]) => value), '60')) * 1e9
  const limit = bounded(options.limit, ['10', '25', '50', '100', '250', '1000'], '100')
  const window = bounded(options.window, timeRangeOptions.map(([value]) => value), '3600')
  const where: string[] = []
  if (window !== '0') where.push(`${time} >= cast(epoch(cast(current_timestamp AS TIMESTAMP)) * 1000000000 AS BIGINT) - ${Number(window) * 1e9}`)
  if (options.service) where.push(`service_name = ${literal(options.service)}`)
  if (isSpanOrTrace && options.operation) where.push(`name = ${literal(options.operation)}`)
  if (source === 'metrics') {
    if (!options.metric) throw new Error('Choose a metric before generating SQL.')
    where.push(`name = ${literal(options.metric)}`)
    // Histogram percentile rows must never be averaged together.
    where.push(`(type <> 'histogram' OR json_extract_string(attributes, '$.percentile') = ${literal(bounded(options.percentile, ['p50', 'p95', 'p99'], 'p95'))})`)
  }
  if (options.filter === 'errors') where.push(source === 'logs' ? 'severity >= 17' : isSpanOrTrace ? 'status_code = 2' : 'TRUE')
  if (isSpanOrTrace && options.filter === 'success') where.push('status_code = 1')
  if (isSpanOrTrace && options.filter === 'non_error') where.push('status_code IN (0, 1)')
  if (isSpanOrTrace && options.filter === 'unset') where.push('status_code = 0')
  if (options.filter === 'warnings' && source === 'logs') where.push('severity >= 13')
  if (options.filter === 'server' && source === 'spans') where.push('kind = 2')
  if (options.filter === 'client' && source === 'spans') where.push('kind = 3')
  if (options.text) where.push(`contains(lower(concat_ws(' ', ${source === 'logs' ? 'body, service_name, attributes' : 'name, service_name, attributes'})), ${literal(options.text.toLowerCase())})`)
  const from = `FROM ${source === 'traces' ? traceRelation(Boolean(options.text)) : source}${where.length ? `\nWHERE ${where.join('\n  AND ')}` : ''}`
  const measureKey = measureOptions(source).some(([key]) => key === options.measure) ? options.measure : source === 'metrics' ? 'latest' : 'count'
  const error = source === 'logs' ? 'severity >= 17' : 'status_code = 2'
  const field = source === 'metrics' ? 'value' : 'duration_ns / 1000000.0'
  const measures: Record<string, string> = { count: 'count(*)', errors: `count(*) FILTER (WHERE ${error})`, error_rate: `coalesce(100.0 * count(*) FILTER (WHERE ${error}) / nullif(count(*), 0), 0)`, avg: `avg(${field})`, min: `min(${field})`, max: `max(${field})`, latest: 'arg_max(value, timestamp_ns)', p50: `quantile_cont(${field}, 0.5)`, p95: `quantile_cont(${field}, 0.95)`, p99: `quantile_cont(${field}, 0.99)` }
  Object.assign(measures, { success: 'count(*) FILTER (WHERE status_code = 1)', non_error: 'count(*) FILTER (WHERE status_code IN (0, 1))', unset: 'count(*) FILTER (WHERE status_code = 0)', success_rate: 'coalesce(100.0 * count(*) FILTER (WHERE status_code = 1) / nullif(count(*), 0), 0)', sum: `sum(${field})`, span_count: 'coalesce(sum(span_count), 0)', avg_spans: 'avg(span_count)', max_spans: 'max(span_count)' })
  const measure = measures[measureKey]
  const group = groupOptions(source).some(([key]) => key === options.group) ? options.group : ''
  const metricSeries = source === 'metrics' && !group ? 'attributes' : group
  const timestamp = `(${time} // ${interval}) * ${interval} AS timestamp_ns`
  const order = options.order === 'oldest' ? `${time} ASC` : options.order === 'slowest' && isSpanOrTrace ? 'duration_ns DESC' : options.order === 'severity' && source === 'logs' ? `severity DESC, ${time} DESC` : `${time} DESC`
  const columns = source === 'traces' ? 'trace_id, name, service_name, start_ns, duration_ns, span_count, status_code' : source === 'spans' ? 'trace_id, span_id, service_name, name, duration_ns, status_code' : source === 'logs' ? 'timestamp_ns, severity, body, service_name, trace_id, span_id' : 'timestamp_ns, name, service_name, type, unit, value, attributes'
  switch (type) {
    case 'single_value': return `SELECT ${measure} AS value\n${from}`
    case 'time_series': return `SELECT ${timestamp}, ${measure} AS value${metricSeries ? `, ${metricSeries} AS group_value` : ''}\n${from}\nGROUP BY 1${metricSeries ? ', 3' : ''}\nORDER BY 1${metricSeries ? ', 3' : ''}\nLIMIT 1000`
    case 'entity_list': return `SELECT ${group || 'service_name'} AS label, ${measure} AS primary_value\n${from}\nGROUP BY 1\nORDER BY primary_value ${options.order === 'lowest' ? 'ASC' : 'DESC'}\nLIMIT ${limit}`
    case 'heatmap': {
      const width = bounded(options.bucket, ['1', '5', '10', '50', '100', '500', '1000'], '10')
      const bucketField = source === 'logs' ? 'severity' : field
      return `SELECT ${timestamp}, floor((${bucketField}) / ${width}) * ${width} AS ${isSpanOrTrace ? 'bucket_ms' : 'y'}, count(*) AS value\n${from}\nGROUP BY 1, 2\nORDER BY 1, 2\nLIMIT 1000`
    }
    default: return `SELECT ${columns}\n${from}\nORDER BY ${order}\nLIMIT ${limit}`
  }
}
