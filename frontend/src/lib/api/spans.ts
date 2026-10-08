import type { ListSpansQuery } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { SpanRow, SpanGroup } from './models'

const isSpanRow = (row: SpanRow | SpanGroup): row is SpanRow => 'span_id' in row
const isSpanGroup = (row: SpanRow | SpanGroup): row is SpanGroup => 'count' in row

export const spans = {
  list: (params?: ListSpansQuery) =>
    unwrap(openapiClient.GET('/api/spans', { params: { query: params } })).then((response) => ({
      ...response,
      data: response.data.filter(isSpanRow),
    })),
  groups: (params?: ListSpansQuery) => {
    return unwrap(
      openapiClient.GET('/api/spans', { params: { query: { ...params, view: 'grouped' } } }),
    ).then((response) => ({ ...response, data: response.data.filter(isSpanGroup) }))
  },
  get: (spanId: string) =>
    unwrap(openapiClient.GET('/api/spans/{spanId}', { params: { path: { spanId } } })),
}
