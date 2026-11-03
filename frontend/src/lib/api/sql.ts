import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { FormatSQLInput, AnalyzeSQLInput } from './requests'

export const sql = {
  analyze: (body: AnalyzeSQLInput) => unwrap(openapiClient.POST('/api/sql/analyze', { body })),
  format: (body: FormatSQLInput) => unwrap(openapiClient.POST('/api/sql/format', { body })),
}
