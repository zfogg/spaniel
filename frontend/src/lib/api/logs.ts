import type { ListLogsQuery } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'

export const logs = {
  list: (params?: ListLogsQuery) => {
    return unwrap(openapiClient.GET('/api/logs', { params: { query: params } }))
  },
}
