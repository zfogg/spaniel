import { openapiClient } from '../openapi'
import { unwrap } from './transport'
import type { FormatSQLInput } from './requests'

export const sql = {
  format: (body: FormatSQLInput) => unwrap(openapiClient.POST('/api/sql/format', { body })),
}
