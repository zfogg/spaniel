import type { ListNotificationsQuery } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'

export const notifications = {
  list: ({ page = 1, limit = 30, source }: ListNotificationsQuery = {}) =>
    unwrap(openapiClient.GET('/api/notifications', { params: { query: { page, limit, source } } })),
  read: (id: string) =>
    unwrap(openapiClient.POST('/api/notifications/{id}/read', { params: { path: { id } } })),
  acknowledge: (id: string) =>
    unwrap(
      openapiClient.POST('/api/notifications/{id}/acknowledge', {
        params: { path: { id } },
      }),
    ),
}
