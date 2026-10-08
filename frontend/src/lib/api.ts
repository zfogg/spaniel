// Stable public entry point. Endpoint implementations use generated OpenAPI contracts.
export type * from './api/models'
export type * from './api/requests'
import {
  databaseSchema,
  services,
  lint,
  stats,
  serviceMap,
  issues,
  health,
  sources,
  forwarders,
  settings,
  storage,
  search,
} from './api/system'
import { dashboards } from './api/dashboards'
import { alerts } from './api/alerts'
import { notifications } from './api/notifications'
import { traces } from './api/traces'
import { spans } from './api/spans'
import { logs } from './api/logs'
import { sessions } from './api/sessions'
import { coverage } from './api/coverage'
import { metrics } from './api/metrics'

export const api = {
  databaseSchema,
  dashboards,
  alerts,
  notifications,
  traces,
  spans,
  logs,
  services,
  sessions,
  lint,
  stats,
  serviceMap,
  issues,
  health,
  sources,
  forwarders,
  settings,
  storage,
  coverage,
  metrics,
  search,
}
