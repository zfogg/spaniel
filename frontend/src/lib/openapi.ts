import createClient from 'openapi-fetch'

import type { paths } from '../generated/openapi'

// This is the browser's contract-aware transport. Keep the base URL relative so
// Vite's development proxy and the embedded production UI use the same client.
export const openapiClient = createClient<paths>({ baseUrl: '' })

export type { components, operations, paths } from '../generated/openapi'
