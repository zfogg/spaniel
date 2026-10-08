import type { PatchSessionInput } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'

export const sessions = {
  list: () => unwrap(openapiClient.GET('/api/sessions')),
  get: (id: string) =>
    unwrap(openapiClient.GET('/api/sessions/{sessionId}', { params: { path: { sessionId: id } } })),
  getActive: () => unwrap(openapiClient.GET('/api/sessions/active')),
  create: (label?: string) => unwrap(openapiClient.POST('/api/sessions', { body: { label } })),
  activate: (id: string) =>
    unwrap(
      openapiClient.POST('/api/sessions/{sessionId}/activate', {
        params: { path: { sessionId: id } },
        body: {},
      }),
    ),
  baseline: (id: string, isBaseline: boolean) =>
    unwrap(
      openapiClient.POST('/api/sessions/{sessionId}/baseline', {
        params: { path: { sessionId: id } },
        body: { is_baseline: isBaseline },
      }),
    ),
  patch: (id: string, body: PatchSessionInput) =>
    unwrap(
      openapiClient.PATCH('/api/sessions/{sessionId}', {
        params: { path: { sessionId: id } },
        body,
      }),
    ),
  delete: (id: string) =>
    unwrap(
      openapiClient.DELETE('/api/sessions/{sessionId}', { params: { path: { sessionId: id } } }),
    ),
  import: (label: string, format: string, data: string) =>
    unwrap(
      openapiClient.POST('/api/sessions/import', {
        params: { query: { label, format } },
        body: data,
      }),
    ),
}
