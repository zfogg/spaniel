import { describe, expect, it } from 'vitest'
import {
  buildPanelSQL,
  builderDefaults,
  intervalOptions,
  timeRangeOptions,
  measureOptions,
  type Source,
} from './panel-builder'

const types = [
  'single_value',
  'time_series',
  'table',
  'heatmap',
  'entity_list',
  'span_list',
  'trace_list',
  'log_list',
]
describe('panel SQL builder', () => {
  it('counts traces rather than spans and filters after full-trace aggregation', () => {
    const sql = buildPanelSQL('single_value', {
      ...builderDefaults,
      source: 'traces',
      measure: 'success',
      service: 'api',
    })
    expect(sql).toContain('GROUP BY session_id, trace_id) AS traces')
    expect(sql).toContain('max(end_ns) - min(start_ns) AS duration_ns')
    expect(sql).toContain('count(*) FILTER (WHERE status_code = 1)')
    expect(sql.indexOf("service_name = 'api'")).toBeGreaterThan(
      sql.indexOf('GROUP BY session_id, trace_id'),
    )
    expect(sql).not.toContain('string_agg')
    expect(buildPanelSQL('trace_list', { ...builderDefaults, text: 'child' })).toContain(
      'string_agg',
    )
  })
  it('distinguishes explicit success, unset and non-error span counts', () => {
    expect(buildPanelSQL('single_value', { ...builderDefaults, measure: 'success' })).toContain(
      'WHERE status_code = 1',
    )
    expect(buildPanelSQL('single_value', { ...builderDefaults, measure: 'unset' })).toContain(
      'WHERE status_code = 0',
    )
    expect(buildPanelSQL('single_value', { ...builderDefaults, measure: 'non_error' })).toContain(
      'WHERE status_code IN (0, 1)',
    )
  })
  it('supports every offered interval and time range through three years', () => {
    for (const [interval] of intervalOptions) {
      expect(buildPanelSQL('time_series', { ...builderDefaults, interval })).toContain(
        `// ${Number(interval) * 1e9}`,
      )
    }
    for (const [window] of timeRangeOptions) {
      const sql = buildPanelSQL('single_value', { ...builderDefaults, window })
      if (window === '0') expect(sql).not.toContain('current_timestamp')
      else expect(sql).toContain(`- ${Number(window) * 1e9}`)
    }
    expect(intervalOptions[intervalOptions.length - 1][0]).toBe('94608000')
  })
  for (const type of types)
    it(`generates read-only ${type} SQL`, () => {
      const sql = buildPanelSQL(type, builderDefaults)
      expect(sql).toMatch(/^(SELECT|WITH) /)
      expect(sql).not.toContain('telemetry_')
      expect(sql).not.toContain('make_timestamp_ns')
    })
  it('applies interval, aggregation, grouping and escaped filters', () => {
    const sql = buildPanelSQL('time_series', {
      ...builderDefaults,
      interval: '300',
      measure: 'p99',
      group: 'name',
      service: "shop's",
      text: "can't",
      filter: 'errors',
    })
    expect(sql).toContain('300000000000')
    expect(sql).toContain('quantile_cont(duration_ns / 1000000.0, 0.99)')
    expect(sql).toContain('name AS group_value')
    expect(sql).toContain("service_name = 'shop''s'")
    expect(sql).toContain("'can''t'")
    expect(sql).toContain('status_code = 2')
  })
  it('separates metric dimensions and histogram percentiles', () => {
    const sql = buildPanelSQL('time_series', {
      ...builderDefaults,
      source: 'metrics',
      metric: 'latency',
      measure: 'latest',
    })
    expect(sql).toContain('attributes AS group_value')
    expect(sql).toContain("'$.percentile') = 'p95'")
    expect(sql).toContain('arg_max(value, timestamp_ns)')
    expect(() => buildPanelSQL('single_value', { ...builderDefaults, source: 'metrics' })).toThrow(
      'Choose a metric',
    )
  })
  it('uses log severity filters and ordering', () => {
    const sql = buildPanelSQL('log_list', {
      ...builderDefaults,
      filter: 'warnings',
      order: 'severity',
      limit: '25',
    })
    expect(sql).toContain('severity >= 13')
    expect(sql).toContain('ORDER BY severity DESC, timestamp_ns DESC')
    expect(sql).toContain('LIMIT 25')
  })
})

// Opt-in integration check: execute every measure/source and renderer shape
// against the local read-only preview API without creating any saved panels.
it.skipIf(!import.meta.env.SPANIEL_VERIFY_URL)(
  'executes generated shapes against DuckDB',
  async () => {
    const base = import.meta.env.SPANIEL_VERIFY_URL
    const dashboards = await fetch(`${base}/api/dashboards`).then((r) => r.json())
    const id = dashboards.data[0].id
    const metrics = await fetch(`${base}/api/metrics`).then((r) => r.json())
    const metric = metrics.data[0]?.name
    // Same trace ID in two sessions must remain two traces. A failed child
    // overrides an OK root; overlapping spans use wall-clock, not summed duration.
    const fixture = `(VALUES
    ('a','t','root','api',0,10000000,1,'{}'),
    ('a','t','child','db',1000000,20000000,2,'{}'),
    ('b','t','root','api',0,30000000,1,'{}'),
    ('a','u','root','api',0,40000000,0,'{}')
  ) AS spans(session_id,trace_id,name,service_name,start_ns,end_ns,status_code,attributes)`
    for (const [measure, expected] of [
      ['count', 3],
      ['errors', 1],
      ['success', 1],
      ['unset', 1],
      ['non_error', 2],
      ['span_count', 4],
      ['max', 40],
    ] as const) {
      const query_sql = buildPanelSQL('single_value', {
        ...builderDefaults,
        source: 'traces',
        measure,
        window: '0',
      }).replace('FROM spans WHERE', `FROM ${fixture} WHERE`)
      const response = await fetch(`${base}/api/dashboards/${id}/query-preview`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query_sql, display_type: 'single_value' }),
      })
      expect(response.ok).toBe(true)
      expect((await response.json()).data.rows[0].value).toBe(expected)
    }
    for (const source of ['spans', 'traces', 'logs', 'metrics'] as Source[]) {
      if (source === 'metrics' && !metric)
        throw new Error('Live metric required for SQL verification')
      for (const type of types) {
        const measures = ['single_value', 'time_series', 'entity_list'].includes(type)
          ? measureOptions(source).map(([key]) => key)
          : ['count']
        for (const measure of measures) {
          const query_sql = buildPanelSQL(type, {
            ...builderDefaults,
            source,
            measure,
            metric,
            window: '300',
          })
          const response = await fetch(`${base}/api/dashboards/${id}/query-preview`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ query_sql, display_type: type }),
          })
          expect(response.ok, `${source}/${type}/${measure}: ${await response.text()}`).toBe(true)
        }
      }
    }
  },
  120_000,
)
