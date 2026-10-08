import { formatDistanceToNow } from 'date-fns'
import { type AlertRule } from '@/lib/api'
export const ruleListTone: Record<string, { line: string; ink: string }> = {
  firing: { line: 'var(--danger)', ink: 'var(--danger-ink)' },
  pending: { line: 'var(--warn)', ink: 'var(--warn-ink)' },
  resolved: { line: 'var(--ok)', ink: 'var(--ok-ink)' },
}

export const ruleState = (rule: AlertRule) =>
  rule.instances?.find((instance) => instance.state === 'firing')?.state ??
  rule.instances?.find((instance) => instance.state === 'pending')?.state ??
  rule.instances?.[0]?.state ??
  'resolved'

export const parseJSON = <T>(value: string, fallback: T): T => {
  try {
    return JSON.parse(value) as T
  } catch {
    return fallback
  }
}

export const formatDuration = (nanoseconds: number) => {
  if (!nanoseconds) return 'None'
  const seconds = nanoseconds / 1e9
  if (seconds < 60) return `${seconds}s`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  return `${seconds / 60}m`
}

export const formatTimestamp = (nanoseconds: number) => new Date(nanoseconds / 1e6).toLocaleString()

export const formatAgo = (nanoseconds: number, now = Date.now()) => {
  const milliseconds = nanoseconds / 1e6
  const difference = now - milliseconds
  if (Math.abs(difference) < 60_000) return difference >= 0 ? 'moments ago' : 'in moments'
  return formatDistanceToNow(milliseconds, { addSuffix: true })
}

export const formatTimestampWithAgo = (nanoseconds: number, now = Date.now()) =>
  `${formatTimestamp(nanoseconds)} · ${formatAgo(nanoseconds, now)}`

export async function showNativeBrowserNotification(title: string, body?: string) {
  if (!('Notification' in window)) return
  let permission = Notification.permission
  if (permission === 'default') permission = await Notification.requestPermission()
  if (permission === 'granted') {
    new Notification(`Spaniel · ${title}`, { body: body ?? 'Browser notification test' })
  }
}

export type Draft = {
  name: string
  query: string
  conditionKind: 'threshold' | 'count' | 'no_data' | 'log_match' | 'any_of' | 'all_of'
  operator: string
  threshold: string
  pattern: string
  sourceRuleIDs: string
  groupBy: string
  pendingFor: string
  cooldown: string
  repeatInterval: string
  severity: 'info' | 'warning' | 'critical'
  enabled: boolean
  browserEnabled: boolean
  pushoverEnabled: boolean
  discoveryQuery: string
  discoveryEvery: string
  discoveryStaleAfter: string
  annotations: string
}

export type HistoryFilters = {
  search: string
  rule_id: string
  state: string
  kind: string
  severity: string
  group_key: string
  from: string
  to: string
}

export const emptyHistoryFilters = (): HistoryFilters => ({
  search: '',
  rule_id: '',
  state: '',
  kind: '',
  severity: '',
  group_key: '',
  from: '',
  to: '',
})

export const durationInput = (ns: number) => (ns ? `${ns / 1e9}s` : '')

export const alertSeverity = (value: string): Draft['severity'] =>
  value === 'info' || value === 'critical' || value === 'warning' ? value : 'warning'

export const durationNS = (value: string, field: string) => {
  if (!value.trim()) return 0
  const match = value.trim().match(/^(\d+(?:\.\d+)?)(ms|s|m|h)$/)
  if (!match) throw new Error(`${field} must be a duration such as 30s, 5m, or 1h`)
  const units: Record<string, number> = { ms: 1e6, s: 1e9, m: 60e9, h: 3600e9 }
  return Number(match[1]) * units[match[2]]
}

export const draftFor = (rule: AlertRule): Draft => {
  const condition = parseJSON<{
    kind?: 'threshold' | 'count' | 'no_data' | 'log_match' | 'any_of' | 'all_of'
    operator?: string
    value?: number
    pattern?: string
    rule_ids?: string[]
  }>(rule.condition_json, {})
  return {
    name: rule.name,
    query: rule.query_sql,
    conditionKind: condition.kind ?? 'threshold',
    operator: condition.operator ?? '>',
    threshold: String(condition.value ?? 0),
    pattern: condition.pattern ?? '',
    sourceRuleIDs: (condition.rule_ids ?? []).join(', '),
    groupBy: parseJSON<string[]>(rule.group_by_json, []).join(', '),
    pendingFor: durationInput(rule.pending_for_ns),
    cooldown: durationInput(rule.cooldown_ns),
    repeatInterval: durationInput(rule.repeat_interval_ns),
    severity: alertSeverity(rule.severity),
    enabled: rule.enabled,
    browserEnabled: rule.browser_enabled,
    pushoverEnabled: rule.pushover_enabled,
    discoveryQuery: rule.instance_discovery_sql,
    discoveryEvery: durationInput(rule.instance_discovery_interval_ns),
    discoveryStaleAfter: durationInput(rule.instance_discovery_stale_after_ns),
    annotations: JSON.stringify(
      parseJSON<Record<string, string>>(rule.annotations_json, {}),
      null,
      2,
    ),
  }
}

export const emptyRule = (): AlertRule => ({
  id: '',
  name: 'New alert',
  query_sql: 'SELECT count(*) AS value FROM spans',
  query_version: 1,
  condition_json: '{"kind":"threshold","operator":">","value":0}',
  group_by_json: '[]',
  annotations_json: '{}',
  pending_for_ns: 0,
  cooldown_ns: 300000000000,
  repeat_interval_ns: 0,
  severity: 'warning',
  enabled: true,
  browser_enabled: true,
  pushover_enabled: true,
  instance_discovery_sql: '',
  instance_discovery_interval_ns: 0,
  instance_discovery_stale_after_ns: 0,
  instance_discovery_last_run_at: 0,
  last_evaluated_at: 0,
  last_success_at: 0,
  last_duration_ns: 0,
  next_evaluation_at: 0,
  last_error: '',
  source_file: '',
  source_hash: '',
  created_at: 0,
  updated_at: 0,
  instances: [],
})

export const draftPayload = (draft: Draft) => {
  let annotations: Record<string, string>
  try {
    annotations = JSON.parse(draft.annotations) as Record<string, string>
  } catch {
    throw new Error('Annotations must be a JSON object.')
  }
  if (!draft.name.trim()) throw new Error('Rule name is required.')
  if (!draft.query.trim()) throw new Error('SQL query is required.')
  const threshold = Number(draft.threshold)
  if (draft.conditionKind !== 'no_data' && !Number.isFinite(threshold))
    throw new Error('Threshold must be a number.')
  if (draft.conditionKind === 'log_match' && !draft.pattern.trim())
    throw new Error('Log pattern is required.')
  const sourceRuleIDs = draft.sourceRuleIDs
    .split(',')
    .map((value) => value.trim())
    .filter(Boolean)
  if (['any_of', 'all_of'].includes(draft.conditionKind) && !sourceRuleIDs.length)
    throw new Error('At least one source rule ID is required.')
  return {
    name: draft.name.trim(),
    query_sql: draft.query,
    condition:
      draft.conditionKind === 'no_data'
        ? { kind: 'no_data' }
        : draft.conditionKind === 'log_match'
          ? {
              kind: 'log_match',
              pattern: draft.pattern.trim(),
              operator: draft.operator,
              value: threshold,
            }
          : draft.conditionKind === 'any_of' || draft.conditionKind === 'all_of'
            ? { kind: draft.conditionKind, rule_ids: sourceRuleIDs }
            : { kind: draft.conditionKind, operator: draft.operator, value: threshold },
    group_by: draft.groupBy
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean),
    pending_for_ns: durationNS(draft.pendingFor, 'Pending for'),
    cooldown_ns: durationNS(draft.cooldown, 'Cooldown'),
    repeat_interval_ns: durationNS(draft.repeatInterval, 'Repeat interval'),
    severity: draft.severity,
    enabled: draft.enabled,
    browser_enabled: draft.browserEnabled,
    pushover_enabled: draft.pushoverEnabled,
    instance_discovery: draft.discoveryQuery.trim()
      ? {
          query: draft.discoveryQuery,
          every_ns: durationNS(draft.discoveryEvery, 'Discovery interval'),
          stale_after_ns: durationNS(draft.discoveryStaleAfter, 'Discovery stale after'),
        }
      : undefined,
    annotations,
  }
}
