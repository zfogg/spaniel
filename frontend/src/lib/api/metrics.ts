import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { MetricSeriesQuery } from './requests'

export const metrics = {
  list: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/metrics', { params: { query: { sessionId } } })),
  cardinality: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/metrics/cardinality', { params: { query: { sessionId } } })),
  series: (params: MetricSeriesQuery) =>
    unwrap(
      openapiClient.GET('/api/metrics/series', {
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
