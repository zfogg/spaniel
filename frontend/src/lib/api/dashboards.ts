import type {
  CreateDashboardInput,
  PatchDashboardInput,
  PreviewDashboardQueryInput,
  CreateDashboardPanelInput,
  PatchDashboardPanelInput,
  CreateDashboardVariableInput,
} from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { Dashboard } from './models'
import type { Envelope } from './transport'

export const dashboards = {
  list: () => unwrap(openapiClient.GET('/api/dashboards')),
  get: (id: string) =>
    unwrap(openapiClient.GET('/api/dashboards/{id}', { params: { path: { id } } })),
  create: (body: CreateDashboardInput) => unwrap(openapiClient.POST('/api/dashboards', { body })),
  update: (id: string, body: PatchDashboardInput) =>
    unwrap(openapiClient.PATCH('/api/dashboards/{id}', { params: { path: { id } }, body })),
  remove: (id: string) =>
    unwrap(openapiClient.DELETE('/api/dashboards/{id}', { params: { path: { id } } })),
  config: async (id: string) => {
    const response = await fetch(`/api/dashboards/${id}/config`)
    if (!response.ok) throw new Error(await response.text())
    return response.text()
  },
  importConfig: async (yaml: string) => {
    const response = await fetch('/api/dashboards/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/yaml' },
      body: yaml,
    })
    if (!response.ok) throw new Error(await response.text())
    return response.json() as Promise<Envelope<Dashboard>>
  },
  preview: (id: string, body: PreviewDashboardQueryInput, signal?: AbortSignal) =>
    unwrap(
      openapiClient.POST('/api/dashboards/{id}/query-preview', {
        params: { path: { id } },
        body,
        signal,
      }),
    ),
  catalog: (signal?: string, search?: string, abortSignal?: AbortSignal) => {
    return unwrap(
      openapiClient.GET('/api/query-catalog', {
        params: { query: { signal, q: search } },
        signal: abortSignal,
      }),
    )
  },
  panel: (id: string, body: CreateDashboardPanelInput) =>
    unwrap(
      openapiClient.POST('/api/dashboards/{id}/panels', {
        params: { path: { id } },
        body,
      }),
    ),
  updatePanel: (id: string, panelId: string, body: PatchDashboardPanelInput) =>
    unwrap(
      openapiClient.PATCH('/api/dashboards/{id}/panels/{panelId}', {
        params: { path: { id, panelId } },
        body,
      }),
    ),
  removePanel: (id: string, panelId: string) =>
    unwrap(
      openapiClient.DELETE('/api/dashboards/{id}/panels/{panelId}', {
        params: { path: { id, panelId } },
      }),
    ),
  movePanel: (id: string, panelId: string, direction: -1 | 1) =>
    unwrap(
      openapiClient.POST('/api/dashboards/{id}/panels/{panelId}/move', {
        params: { path: { id, panelId } },
        body: { direction },
      }),
    ),
  variable: (id: string, body: CreateDashboardVariableInput) =>
    unwrap(
      openapiClient.POST('/api/dashboards/{id}/variables', {
        params: { path: { id } },
        body,
      }),
    ),
  deleteVariable: (id: string, name: string) =>
    unwrap(
      openapiClient.DELETE('/api/dashboards/{id}/variables/{name}', {
        params: { path: { id, name } },
      }),
    ),
  reorder: (ids: string[]) =>
    unwrap(openapiClient.POST('/api/dashboards/reorder', { body: { ids } })),
}
