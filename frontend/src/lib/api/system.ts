import type { SettingsUpdate } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'

export const databaseSchema = {
  get: () => unwrap(openapiClient.GET('/api/database-schema')),
}

export const services = {
  list: () => unwrap(openapiClient.GET('/api/services')),
}

export const lint = {
  list: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/lint', { params: { query: { sessionId } } })),
}

export const stats = {
  get: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/stats', { params: { query: { sessionId } } })),
}

export const serviceMap = {
  get: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/service-map', { params: { query: { sessionId } } })),
}

export const issues = {
  get: (traceId: string) =>
    unwrap(openapiClient.GET('/api/issues', { params: { query: { traceId } } })),
  list: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/issues', { params: { query: { sessionId } } })),
}

export const health = {
  get: () => unwrap(openapiClient.GET('/api/health')),
  // Presents the bearer token to /api/health so the server sets the auth cookie.
  seed: (token: string): Promise<void> =>
    fetch('/api/health', { headers: { Authorization: `Bearer ${token}` } }).then(() => undefined),
}

export const sources = {
  list: () => unwrap(openapiClient.GET('/api/sources')),
}

export const forwarders = {
  list: () => unwrap(openapiClient.GET('/api/forwarders')),
}

export const settings = {
  get: () => unwrap(openapiClient.GET('/api/settings')),
  update: (patchBody: SettingsUpdate) =>
    unwrap(openapiClient.PUT('/api/settings', { body: patchBody })),
  dropAllData: () => unwrap(openapiClient.DELETE('/api/settings/data')),
  compact: () => unwrap(openapiClient.POST('/api/settings/compact', { body: {} })),
  prune: () => unwrap(openapiClient.POST('/api/settings/prune', { body: {} })),
  checkUpdates: () => unwrap(openapiClient.POST('/api/settings/check-updates', { body: {} })),
}

export const storage = {
  get: () => unwrap(openapiClient.GET('/api/storage')),
}

export const search = {
  query: (q: string, sessionId?: string) =>
    unwrap(openapiClient.GET('/api/search', { params: { query: { q, limit: 20, sessionId } } })),
}
