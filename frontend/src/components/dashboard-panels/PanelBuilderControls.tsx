import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  buildPanelSQL,
  builderDefaults,
  groupOptions,
  intervalOptions,
  timeRangeOptions,
  measureOptions,
  type BuilderOptions,
  type Source,
} from './panel-builder'

const inputClass =
  'mt-1 block w-full min-w-0 rounded border border-border bg-background px-2 py-1.5 font-sans text-[11px] text-foreground'
export function PanelBuilderControls({
  display,
  apply,
}: {
  display: string
  apply: (sql: string) => void
}) {
  const [options, setOptions] = useState<BuilderOptions>(builderDefaults)
  const [error, setError] = useState('')
  const source =
    display === 'log_list'
      ? 'logs'
      : display === 'trace_list'
        ? 'traces'
        : display === 'span_list'
          ? 'spans'
          : options.source
  const isSpanOrTrace = source === 'spans' || source === 'traces'
  const aggregate = ['single_value', 'time_series', 'entity_list'].includes(display)
  const services = useQuery({
    queryKey: ['builder-services'],
    queryFn: () => api.services.list().then((x) => x.data),
    staleTime: 60_000,
  })
  const metrics = useQuery({
    queryKey: ['builder-metrics'],
    queryFn: () => api.metrics.list().then((x) => x.data),
    enabled: source === 'metrics',
    staleTime: 60_000,
  })
  const change = (key: keyof BuilderOptions, value: string) => {
    setError('')
    setOptions((current) => ({
      ...current,
      [key]: value,
      ...(key === 'service' ? { metric: '' } : {}),
    }))
  }
  const select = (
    label: string,
    key: keyof BuilderOptions,
    choices: string[][],
    fallback?: string,
  ) => (
    <label className="min-w-0 font-mono text-[10px] text-muted-foreground">
      {key === 'measure' && source === 'spans' ? (
        <Tooltip>
          <TooltipTrigger render={<span tabIndex={0} className="cursor-help" />}>
            {label}
          </TooltipTrigger>
          <TooltipContent className="max-w-xs border border-border bg-popover text-popover-foreground">
            Successful means explicit OK status. Unset is not assumed successful; non-error includes
            both OK and unset.
          </TooltipContent>
        </Tooltip>
      ) : (
        label
      )}
      <select
        value={
          choices.some(([value]) => value === options[key])
            ? options[key]
            : (fallback ?? choices[0]?.[0])
        }
        onChange={(event) => change(key, event.target.value)}
        className={inputClass}
      >
        {choices.map(([value, text]) => (
          <option key={value} value={value}>
            {text}
          </option>
        ))}
      </select>
    </label>
  )
  return (
    <div className="grid grid-cols-2 gap-2">
      {!['span_list', 'trace_list', 'log_list'].includes(display) && (
        <label className="font-mono text-[10px] text-muted-foreground">
          Telemetry source
          <select
            value={source}
            onChange={(event) => {
              const next = event.target.value as Source
              setOptions((current) => ({
                ...current,
                source: next,
                measure: next === 'metrics' ? 'latest' : 'count',
                group: '',
                filter: '',
                order: 'newest',
              }))
            }}
            className={inputClass}
          >
            <option value="spans">Spans</option>
            <option value="traces">Traces</option>
            <option value="logs">Logs</option>
            <option value="metrics">Metrics</option>
          </select>
        </label>
      )}
      {source === 'traces' && (
        <p className="col-span-2 text-[10px] text-muted-foreground">
          One trace per session and trace ID, using all its stored spans. Service and operation
          refer to the earliest span; time range uses trace start. Any failed span makes the trace
          failed; otherwise any explicit OK makes it OK, else unset. Attribute search includes child
          spans.
        </p>
      )}
      {select('Time range', 'window', timeRangeOptions)}
      {select('Service', 'service', [
        ['', 'All services'],
        ...(services.data ?? []).map((value) => [value, value]),
      ])}
      {services.isError && (
        <p role="alert" className="col-span-2 text-xs text-danger">
          Could not load services. You can still edit service filters in SQL.
        </p>
      )}
      {source === 'metrics' && (
        <>
          {select('Metric', 'metric', [
            ['', metrics.isPending ? 'Loading metrics…' : 'Choose a collected metric'],
            ...[
              ...new Set(
                (metrics.data ?? [])
                  .filter((metric) => !options.service || metric.service_name === options.service)
                  .map((metric) => metric.name),
              ),
            ].map((name) => [name, name]),
          ])}
          {select('Histogram percentile', 'percentile', [
            ['p50', 'p50 — median'],
            ['p95', 'p95'],
            ['p99', 'p99'],
          ])}
          <p className="col-span-2 text-[10px] text-muted-foreground">
            Counters use reported values, not inferred rates. Time series keep attribute sets
            separate; histogram percentile applies only to histogram metrics.
          </p>
          {metrics.isError && (
            <p role="alert" className="col-span-2 text-xs text-danger">
              Could not load metrics. Retry later or write SQL directly.
            </p>
          )}
        </>
      )}
      {aggregate &&
        select(
          'Measure',
          'measure',
          measureOptions(source),
          source === 'metrics' ? 'latest' : 'count',
        )}
      {display === 'time_series' && select('Interval', 'interval', intervalOptions)}
      {['time_series', 'entity_list'].includes(display) &&
        select(
          display === 'entity_list' ? 'Entity' : 'Group by',
          'group',
          groupOptions(source).filter(([key]) => display !== 'entity_list' || key !== ''),
          display === 'entity_list' ? 'service_name' : '',
        )}
      {source !== 'metrics' &&
        select('Filter', 'filter', [
          ['', 'All records'],
          [
            'errors',
            source === 'logs'
              ? 'Errors and fatal logs'
              : source === 'traces'
                ? 'Failed traces'
                : 'Failed spans',
          ],
          ...(source === 'logs'
            ? [['warnings', 'Warnings and above']]
            : [
                ['success', 'Explicit OK'],
                ['non_error', 'Non-error (OK or unset)'],
                ['unset', 'Unset status'],
                ...(source === 'spans'
                  ? [
                      ['server', 'Server spans'],
                      ['client', 'Client spans'],
                    ]
                  : []),
              ]),
        ])}
      {isSpanOrTrace && (
        <label className="font-mono text-[10px] text-muted-foreground">
          Operation (exact, optional)
          <input
            value={options.operation}
            onChange={(e) => change('operation', e.target.value)}
            className={inputClass}
            placeholder="e.g. GET /orders"
          />
        </label>
      )}
      <label className="font-mono text-[10px] text-muted-foreground">
        Text / attribute search
        <input
          value={options.text}
          onChange={(e) => change('text', e.target.value)}
          className={inputClass}
          placeholder="Optional matching text"
        />
      </label>
      {display === 'heatmap' &&
        select(
          isSpanOrTrace
            ? 'Bucket width (ms)'
            : source === 'metrics'
              ? 'Bucket width (metric units)'
              : 'Severity bucket width',
          'bucket',
          ['1', '5', '10', '50', '100', '500', '1000'].map((value) => [value, value]),
        )}
      {['table', 'span_list', 'trace_list', 'log_list', 'entity_list'].includes(display) && (
        <>
          {select(
            'Sort order',
            'order',
            display === 'entity_list'
              ? [
                  ['highest', 'Highest value first'],
                  ['lowest', 'Lowest value first'],
                ]
              : [
                  ['newest', 'Newest first'],
                  ['oldest', 'Oldest first'],
                  ...(isSpanOrTrace
                    ? [['slowest', 'Slowest first']]
                    : source === 'logs'
                      ? [['severity', 'Most severe first']]
                      : []),
                ],
          )}
          {select(
            'Row limit',
            'limit',
            ['10', '25', '50', '100', '250', '1000'].map((value) => [value, `${value} rows`]),
          )}
        </>
      )}
      <p className="col-span-2 text-[10px] text-muted-foreground">
        These controls generate a new query. Applying it replaces the SQL below; manual edits remain
        untouched until then.
      </p>
      {error && (
        <p role="alert" className="col-span-2 text-xs text-danger">
          {error}
        </p>
      )}
      <button
        type="button"
        className="col-span-2 justify-self-end cursor-pointer rounded border border-border bg-background px-3 py-1.5 text-[11px] hover:bg-muted"
        onClick={() => {
          try {
            apply(buildPanelSQL(display, options))
            setError('')
          } catch (error) {
            setError(error instanceof Error ? error.message : String(error))
          }
        }}
      >
        Use generated SQL
      </button>
    </div>
  )
}
