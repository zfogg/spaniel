import type { Route } from '@playwright/test'
import type { components } from '../src/generated/openapi'

// Editor/inspector tests mock successful analysis; mapping and parser behavior
// are covered separately by the generated query-analysis contract fixtures.
export function replySQLAnalysis(route: Route) {
  const data = {
    sql: route.request().postDataJSON().sql ?? '',
    status: 'captured',
    reason: '',
    diagnostics: [],
  } satisfies components['schemas']['SQLDisplayAnalysis']
  return route.fulfill({ json: { data } })
}
