import type { operations } from '../openapi'

// Request shapes come from OpenAPI, so endpoint wrappers cannot drift from the server contract.
type JsonBody<K extends keyof operations> =
  NonNullable<operations[K]['requestBody']> extends {
    content: { 'application/json': infer Body }
  }
    ? Body
    : never
type QueryParameters<K extends keyof operations> = operations[K]['parameters'] extends {
  query?: infer Query
}
  ? NonNullable<Query>
  : never

export type ListAlertsQuery = QueryParameters<'listAlerts'>
export type CreateAlertInput = JsonBody<'createAlert'>
export type PatchAlertInput = JsonBody<'patchAlert'>
export type PreviewAlertDraftInput = JsonBody<'previewAlertDraft'>
export type ListAlertHistoryQuery = QueryParameters<'listAlertHistory'>
export type CreateAlertSilenceInput = JsonBody<'createAlertSilence'>
export type PatchAlertSilenceInput = JsonBody<'patchAlertSilence'>
export type CreateCoverageSpecInput = JsonBody<'createCoverageSpec'>
export type ReplaceCoverageSpecInput = JsonBody<'replaceCoverageSpec'>
export type CreateDashboardInput = JsonBody<'createDashboard'>
export type PatchDashboardInput = JsonBody<'patchDashboard'>
export type PreviewDashboardQueryInput = JsonBody<'previewDashboardQuery'>
export type CreateDashboardPanelInput = JsonBody<'createDashboardPanel'>
export type PatchDashboardPanelInput = JsonBody<'patchDashboardPanel'>
export type CreateDashboardVariableInput = JsonBody<'createDashboardVariable'>
export type ListLogsQuery = QueryParameters<'listLogs'>
export type ListNotificationsQuery = QueryParameters<'listNotifications'>
export type PatchSessionInput = JsonBody<'patchSession'>
export type ListSpansQuery = QueryParameters<'listSpans'>
export type SettingsUpdate = JsonBody<'putSettings'>
export type ListTracesQuery = QueryParameters<'listTraces'>

// Retain the browser-facing camelCase names while deriving their value types from the wire contract.
type AlertEventsParameters = QueryParameters<'listAlertEvents'>
export type AlertEventsQuery = Omit<AlertEventsParameters, 'group_key'> & {
  groupKey?: AlertEventsParameters['group_key']
}
type MetricSeriesParameters = QueryParameters<'getMetricSeries'>
export type MetricSeriesQuery = Omit<MetricSeriesParameters, 'with_traces' | 'attributes'> & {
  withTraces?: MetricSeriesParameters['with_traces']
  dimensionFilters?: MetricSeriesParameters['attributes']
}
