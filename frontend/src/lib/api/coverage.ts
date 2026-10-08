import type { CreateCoverageSpecInput, ReplaceCoverageSpecInput } from './requests'
import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { Envelope } from './transport'
import type { CoverageSpec } from './models'

// These operations still generate an untyped envelope. Preserve the existing
// schema-backed response narrowing until their OpenAPI response schemas are specialized.

export const coverage = {
  get: (sessionId?: string) =>
    unwrap(openapiClient.GET('/api/coverage', { params: { query: { sessionId } } })),
  specs: () =>
    unwrap(openapiClient.GET('/api/coverage/specs')) as Promise<Envelope<CoverageSpec[]>>,
  createSpec: (body: CreateCoverageSpecInput) =>
    unwrap(openapiClient.POST('/api/coverage/specs', { body })) as Promise<Envelope<CoverageSpec>>,
  replaceSpec: (id: string, body: ReplaceCoverageSpecInput) =>
    unwrap(
      openapiClient.PUT('/api/coverage/specs/{id}', { params: { path: { id } }, body }),
    ) as Promise<Envelope<CoverageSpec>>,
  deleteSpec: (id: string) =>
    unwrap(
      openapiClient.DELETE('/api/coverage/specs/{id}', { params: { path: { id } } }),
    ) as Promise<Envelope<{ ok: boolean }>>,
  getSpec: (id: string) =>
    unwrap(openapiClient.GET('/api/coverage/specs/{id}', { params: { path: { id } } })) as Promise<
      Envelope<CoverageSpec>
    >,
}
