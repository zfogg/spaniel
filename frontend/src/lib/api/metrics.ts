import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { MetricSeriesQuery } from './requests'

export const metrics = {
  list: (sessionId?: string, signal?: AbortSignal) =>
    unwrap(openapiClient.GET('/api/metrics', { params: { query: { sessionId } }, signal })),
  cardinality: (sessionId?: string, signal?: AbortSignal) =>
    unwrap(
      openapiClient.GET('/api/metrics/cardinality', { params: { query: { sessionId } }, signal }),
    ),
  series: (params: MetricSeriesQuery, signal?: AbortSignal) =>
    unwrap(
      openapiClient.GET('/api/metrics/series', {
        signal,
        params: {
          query: {
            name: params.name,
            service: params.service,
            sessionId: params.sessionId,
            from: params.from,
            to: params.to,
            operation: params.operation,
            with_traces: params.withTraces,
            attributes: params.dimensionFilters,
          },
        },
      }),
    ),
}
