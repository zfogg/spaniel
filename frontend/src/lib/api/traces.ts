import type { ListTracesQuery } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'

export const traces = {
  list: (params: ListTracesQuery = {}) => {
    return unwrap(openapiClient.GET('/api/traces', { params: { query: params } }))
  },
  get: (traceId: string) =>
    unwrap(openapiClient.GET('/api/traces/{traceId}', { params: { path: { traceId } } })),
  exportUrl: (traceId: string) => `/api/traces/${traceId}/export`,
}
