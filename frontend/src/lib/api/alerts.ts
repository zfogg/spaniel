import type {
  ListAlertsQuery,
  CreateAlertInput,
  PatchAlertInput,
  PreviewAlertDraftInput,
  ListAlertHistoryQuery,
  CreateAlertSilenceInput,
  PatchAlertSilenceInput,
  AlertEventsQuery,
} from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { AlertRule } from './models'
import type { Envelope } from './transport'

export const alerts = {
  list: ({ page = 1, limit = 15, state, search }: ListAlertsQuery = {}) => {
    return unwrap(
      openapiClient.GET('/api/alerts', { params: { query: { page, limit, state, search } } }),
    )
  },
  get: (id: string) => unwrap(openapiClient.GET('/api/alerts/{id}', { params: { path: { id } } })),
  create: (body: CreateAlertInput) => unwrap(openapiClient.POST('/api/alerts', { body })),
  update: (id: string, body: PatchAlertInput) =>
    unwrap(openapiClient.PATCH('/api/alerts/{id}', { params: { path: { id } }, body })),
  duplicate: (id: string) =>
    unwrap(
      openapiClient.POST('/api/alerts/{id}/duplicate', { params: { path: { id } }, body: {} }),
    ),
  testNotification: (id: string, destination: 'browser' | 'pushover') =>
    unwrap(
      openapiClient.POST('/api/alerts/{id}/test-notification', {
        params: { path: { id } },
        body: { destination },
      }),
    ),
  remove: (id: string) =>
    unwrap(openapiClient.DELETE('/api/alerts/{id}', { params: { path: { id } } })),
  acknowledge: (id: string) =>
    unwrap(
      openapiClient.POST('/api/alerts/{id}/acknowledge', { params: { path: { id } }, body: {} }),
    ),
  acknowledgeInstance: (id: string, groupKey: string, note = '') =>
    unwrap(
      openapiClient.POST('/api/alerts/{id}/instances/acknowledge', {
        params: { path: { id } },
        body: { group_key: groupKey, note },
      }),
    ),
  unacknowledgeInstance: (id: string, groupKey: string) =>
    unwrap(
      openapiClient.POST('/api/alerts/{id}/instances/unacknowledge', {
        params: { path: { id } },
        body: { group_key: groupKey },
      }),
    ),
  preview: (id: string) =>
    unwrap(openapiClient.POST('/api/alerts/{id}/preview', { params: { path: { id } }, body: {} })),
  previewDraft: (body: PreviewAlertDraftInput) =>
    unwrap(openapiClient.POST('/api/alerts/preview', { body })),
  config: async (id: string) => {
    const r = await fetch(`/api/alerts/${id}/config`)
    if (!r.ok) throw new Error(await r.text())
    return r.text()
  },
  importConfig: async (yaml: string) => {
    const r = await fetch('/api/alerts/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/yaml' },
      body: yaml,
    })
    if (!r.ok) throw new Error(await r.text())
    return r.json() as Promise<Envelope<AlertRule>>
  },
  reload: () => unwrap(openapiClient.POST('/api/alerts/reload', { body: {} })),
  events: (id: string, { groupKey, page = 1, limit = 15 }: AlertEventsQuery = {}) => {
    return unwrap(
      openapiClient.GET('/api/alerts/{id}/events', {
        params: { path: { id }, query: { group_key: groupKey, page, limit } },
      }),
    )
  },
  history: (filters: ListAlertHistoryQuery = {}) => {
    return unwrap(openapiClient.GET('/api/alerts/history', { params: { query: filters } }))
  },
  silence: (id: string, body: CreateAlertSilenceInput) =>
    unwrap(openapiClient.POST('/api/alerts/{id}/silences', { params: { path: { id } }, body })),
  updateSilence: (id: string, silenceID: string, body: PatchAlertSilenceInput) =>
    unwrap(
      openapiClient.PATCH('/api/alerts/{id}/silences/{silenceID}', {
        params: { path: { id, silenceID } },
        body,
      }),
    ),
  removeSilence: (id: string, silenceID: string) =>
    unwrap(
      openapiClient.DELETE('/api/alerts/{id}/silences/{silenceID}', {
        params: { path: { id, silenceID } },
      }),
    ),
}
