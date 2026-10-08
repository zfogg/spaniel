import {
  type Dispatch,
  type ReactNode,
  type SetStateAction,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { BellRing, Search } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { api, type AlertRule, type AlertEvent, type AlertSilence } from '@/lib/api'
import { qk } from '@/lib/query'
import { useWS } from '@/lib/ws'
import { SqlCode, SqlEditor, YamlEditor } from '@/components/ui/HighlightedCode'
import PaginationControls from '@/components/PaginationControls'

const tone: Record<string, string> = {
  firing: 'bg-danger-bg text-danger-ink',
  pending: 'bg-warn-bg text-warn-ink',
  resolved: 'bg-ok-bg text-ok-ink',
  error: 'bg-danger-bg text-danger-ink',
}
const severityTone: Record<string, string> = {
  critical: 'bg-danger-bg text-danger-ink',
  error: 'bg-danger-bg text-danger-ink',
  warning: 'bg-warn-bg text-warn-ink',
  info: 'bg-accent-bg text-accent-ink',
}
const ruleState = (rule: AlertRule) =>
  rule.instances?.find((instance) => instance.state === 'firing')?.state ??
  rule.instances?.find((instance) => instance.state === 'pending')?.state ??
  rule.instances?.[0]?.state ??
  'resolved'
const parseJSON = <T,>(value: string, fallback: T): T => {
  try {
    return JSON.parse(value) as T
  } catch {
    return fallback
  }
}
const formatDuration = (nanoseconds: number) => {
  if (!nanoseconds) return 'None'
  const seconds = nanoseconds / 1e9
  if (seconds < 60) return `${seconds}s`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  return `${seconds / 60}m`
}
const formatTimestamp = (nanoseconds: number) => new Date(nanoseconds / 1e6).toLocaleString()
const formatAgo = (nanoseconds: number, now = Date.now()) => {
  const milliseconds = nanoseconds / 1e6
  const difference = now - milliseconds
  if (Math.abs(difference) < 60_000) return difference >= 0 ? 'moments ago' : 'in moments'
  return formatDistanceToNow(milliseconds, { addSuffix: true })
}
const formatTimestampWithAgo = (nanoseconds: number, now = Date.now()) =>
  `${formatTimestamp(nanoseconds)} · ${formatAgo(nanoseconds, now)}`

let relativeTimeNow = Date.now()
const relativeTimeListeners = new Set<() => void>()
let relativeTimeInterval: ReturnType<typeof setInterval> | undefined

const subscribeToRelativeTime = (listener: () => void) => {
  relativeTimeListeners.add(listener)
  if (!relativeTimeInterval) {
    relativeTimeInterval = setInterval(() => {
      relativeTimeNow = Date.now()
      for (const notify of relativeTimeListeners) notify()
    }, 1_000)
  }
  return () => {
    relativeTimeListeners.delete(listener)
    if (!relativeTimeListeners.size && relativeTimeInterval) {
      clearInterval(relativeTimeInterval)
      relativeTimeInterval = undefined
    }
  }
}

const useRelativeTimeNow = () =>
  useSyncExternalStore(
    subscribeToRelativeTime,
    () => relativeTimeNow,
    () => relativeTimeNow,
  )

function TimestampWithAgo({ nanoseconds }: { nanoseconds: number }) {
  const now = useRelativeTimeNow()
  const timestamp = useMemo(() => formatTimestamp(nanoseconds), [nanoseconds])
  return (
    <span>
      {timestamp} · {formatAgo(nanoseconds, now)}
    </span>
  )
}

function GroupBadge({ groupKey }: { groupKey: string }) {
  return (
    <span className="inline-flex rounded bg-accent-bg px-1.5 py-0.5 font-mono text-[10px] text-accent-ink">
      {groupKey}
    </span>
  )
}

async function showNativeBrowserNotification(title: string, body?: string) {
  if (!('Notification' in window)) return
  let permission = Notification.permission
  if (permission === 'default') permission = await Notification.requestPermission()
  if (permission === 'granted') {
    new Notification(`Spaniel · ${title}`, { body: body ?? 'Browser notification test' })
  }
}

type Draft = {
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
  owner: string
  team: string
  severity: string
  enabled: boolean
  browserEnabled: boolean
  pushoverEnabled: boolean
  discoveryQuery: string
  discoveryEvery: string
  discoveryStaleAfter: string
  annotations: string
}
type HistoryFilters = {
  search: string
  rule_id: string
  state: string
  kind: string
  severity: string
  group_key: string
  from: string
  to: string
}
const emptyHistoryFilters = (): HistoryFilters => ({
  search: '',
  rule_id: '',
  state: '',
  kind: '',
  severity: '',
  group_key: '',
  from: '',
  to: '',
})
const durationInput = (ns: number) => (ns ? `${ns / 1e9}s` : '')
const durationNS = (value: string, field: string) => {
  if (!value.trim()) return 0
  const match = value.trim().match(/^(\d+(?:\.\d+)?)(ms|s|m|h)$/)
  if (!match) throw new Error(`${field} must be a duration such as 30s, 5m, or 1h`)
  const units: Record<string, number> = { ms: 1e6, s: 1e9, m: 60e9, h: 3600e9 }
  return Number(match[1]) * units[match[2]]
}
const draftFor = (rule: AlertRule): Draft => {
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
    owner: rule.owner,
    team: rule.team,
    severity: rule.severity,
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
const emptyRule = (): AlertRule => ({
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
  owner: '',
  team: '',
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
const draftPayload = (draft: Draft) => {
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
    owner: draft.owner.trim(),
    team: draft.team.trim(),
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
export default function Alerts() {
  const [searchParams, setSearchParams] = useSearchParams()
  const historyFiltersFromURL = (): HistoryFilters => ({
    search: searchParams.get('history_search') ?? '',
    rule_id: searchParams.get('history_rule') ?? '',
    state: searchParams.get('history_state') ?? '',
    kind: searchParams.get('history_kind') ?? '',
    severity: searchParams.get('history_severity') ?? '',
    group_key: searchParams.get('history_group') ?? '',
    from: searchParams.get('history_from') ?? '',
    to: searchParams.get('history_to') ?? '',
  })
  const qc = useQueryClient(),
    [tab, setTab] = useState<'board' | 'history'>(
      searchParams.get('tab') === 'history' ? 'history' : 'board',
    ),
    [q, setQ] = useState(searchParams.get('filter') ?? ''),
    [state, setState] = useState(searchParams.get('state') ?? 'attention'),
    [rulePage, setRulePage] = useState(Number(searchParams.get('page')) || 1),
    [historyFilters, setHistoryFilters] = useState<HistoryFilters>(historyFiltersFromURL),
    [historyPage, setHistoryPage] = useState(1),
    [yaml, setYaml] = useState<string | null>(null)
  const selectedId = searchParams.get('id')
  const mode = searchParams.get('mode')
  const setLocation = (id: string | null, nextMode?: string | null) =>
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      // The first rule is the stable default selection, so keep its URL clean.
      if (id && !(rulePage === 1 && id === rules[0]?.id)) next.set('id', id)
      else next.delete('id')
      if (nextMode) next.set('mode', nextMode)
      else next.delete('mode')
      return next
    })
  const creating = mode === 'create',
    importing = mode === 'import'
  const { data: rulesData, error } = useQuery({
    queryKey: qk.alerts({ page: rulePage, state, search: q }),
    queryFn: () => api.alerts.list({ page: rulePage, limit: 15, state, search: q }),
  })
  const rules = rulesData?.data.items ?? []
  const ruleTotal = rulesData?.meta?.total ?? 0
  const ruleSummary = rulesData?.data.summary
  const history = useQuery({
    queryKey: ['alert-history', historyFilters, historyPage],
    queryFn: () =>
      api.alerts
        .history({
          ...historyFilters,
          page: historyPage,
          limit: 50,
          from: historyFilters.from ? new Date(historyFilters.from).getTime() * 1e6 : undefined,
          to: historyFilters.to ? new Date(historyFilters.to).getTime() * 1e6 : undefined,
        })
        .then((x) => x),
    enabled: tab === 'history',
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: qk.alerts() })
    qc.invalidateQueries({ queryKey: ['alert-history'] })
    qc.invalidateQueries({ queryKey: ['alert-events'] })
    qc.invalidateQueries({ queryKey: ['alert-silences'] })
  }
  useWS((e) => {
    if (e.type === 'alert' || e.type === 'alert_sync') refresh()
    // Server ordering puts firing rules first. Bring a newly firing rule to
    // page one so live changes cannot remain invisible on a later page.
    if (e.type === 'alert' && e.payload.transition === 'firing') setRulePage(1)
  })
  const selectedFromPage = rules.find((x) => x.id === selectedId)
  const selectedRule = useQuery({
    queryKey: ['alert-rule', selectedId],
    queryFn: () => api.alerts.get(selectedId!),
    enabled: Boolean(selectedId && !selectedFromPage),
  })
  const selected: AlertRule | undefined = selectedFromPage ?? selectedRule.data?.data ?? rules[0]
  const setSelectedId = (id: string) => setLocation(rules[0]?.id === id ? null : id, null)
  const instanceCounts = ruleSummary?.instance_counts ?? {}
  const ruleCounts = ruleSummary?.rule_counts ?? {}
  const showYaml = async () => {
    if (!selected) return
    try {
      setYaml(await api.alerts.config(selected.id))
      setLocation(selected.id, 'yaml')
    } catch (e) {
      setYaml(e instanceof Error ? e.message : String(e))
    }
  }
  useEffect(() => {
    if (mode !== 'yaml' || !selected || yaml !== null) return
    api.alerts
      .config(selected.id)
      .then(setYaml)
      .catch((error: unknown) => setYaml(error instanceof Error ? error.message : String(error)))
  }, [mode, selected?.id, yaml])
  useEffect(() => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        const setOptional = (key: string, value: string, defaultValue = '') => {
          if (value && value !== defaultValue) next.set(key, value)
          else next.delete(key)
        }
        setOptional('tab', tab, 'board')
        setOptional('filter', q)
        setOptional('state', state, 'attention')
        setOptional('page', String(rulePage), '1')
        setOptional('history_search', historyFilters.search)
        setOptional('history_rule', historyFilters.rule_id)
        setOptional('history_state', historyFilters.state)
        setOptional('history_kind', historyFilters.kind)
        setOptional('history_severity', historyFilters.severity)
        setOptional('history_group', historyFilters.group_key)
        setOptional('history_from', historyFilters.from)
        setOptional('history_to', historyFilters.to)
        return next
      },
      { replace: true },
    )
  }, [historyFilters, q, rulePage, setSearchParams, state, tab])
  return (
    <div className="flex min-h-0 flex-1 overflow-hidden bg-background">
      <section className="w-[52%] min-w-[410px] overflow-auto border-r border-border bg-surface">
        <header className="border-b border-border px-6 py-5">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h1 className="text-xl font-semibold">Alerts</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                Signals needing attention and their history.
              </p>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => {
                  setLocation(null, 'create')
                }}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                New rule
              </button>
              <button
                onClick={() => {
                  setLocation(selectedId, 'import')
                }}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                Import YAML
              </button>
            </div>
          </div>
        </header>
        <div className="p-4">
          <div className="mb-3 flex gap-1">
            <button
              onClick={() => {
                setTab('board')
                setState('attention')
              }}
              className={`rounded px-2 py-1 text-xs ${
                state !== 'notifications' && tab === 'board' ? 'bg-accent-bg' : 'bg-muted'
              }`}
            >
              Needs attention
            </button>
            <button
              onClick={() => {
                setTab('history')
                setState('attention')
              }}
              className={`rounded px-2 py-1 text-xs ${
                state !== 'notifications' && tab === 'history' ? 'bg-accent-bg' : 'bg-muted'
              }`}
            >
              History
            </button>
            <button
              onClick={() => {
                setTab('board')
                setState('notifications')
                setRulePage(1)
              }}
              className={`rounded px-2 py-1 text-xs ${state === 'notifications' ? 'bg-accent-bg' : 'bg-muted'}`}
            >
              Notifications
            </button>
          </div>
          {tab === 'board' && state !== 'notifications' && (
            <div className="mb-3 flex gap-1">
              {['attention', 'all', 'firing', 'pending', 'resolved'].map((x) => (
                <button
                  key={x}
                  onClick={() => {
                    setState(x)
                    setRulePage(1)
                  }}
                  className={`rounded px-2 py-1 text-xs ${state === x ? 'bg-accent-bg' : 'bg-muted'}`}
                >
                  {x}{' '}
                  {x === 'all'
                    ? Object.values(ruleCounts).reduce((count, value) => count + value, 0)
                    : x === 'attention'
                      ? (ruleCounts.firing ?? 0) + (ruleCounts.pending ?? 0)
                      : (ruleCounts[x] ?? 0)}
                </button>
              ))}
            </div>
          )}
          {state !== 'notifications' && (
            <label className="mb-3 flex items-center gap-2 rounded border border-border px-2 py-1.5">
              <Search size={13} />
              <input
                value={tab === 'history' ? historyFilters.search : q}
                onChange={(e) =>
                  tab === 'history'
                    ? (setHistoryFilters((current) => ({ ...current, search: e.target.value })),
                      setHistoryPage(1))
                    : (setQ(e.target.value), setRulePage(1))
                }
                placeholder={tab === 'history' ? 'Search alert history' : 'Filter alert rules'}
                className="w-full bg-transparent text-sm outline-none"
              />
            </label>
          )}
          {error && (
            <p role="alert" className="text-danger">
              Could not load alerts.
            </p>
          )}
          {state === 'notifications' ? (
            <NotificationList />
          ) : tab === 'history' ? (
            <History
              rows={history.data?.data ?? []}
              total={history.data?.meta?.total ?? 0}
              page={historyPage}
              rules={rules}
              filters={historyFilters}
              setFilters={(next) => {
                setHistoryFilters(next)
                setHistoryPage(1)
              }}
              setPage={setHistoryPage}
              onSelect={(id) => {
                setSelectedId(id)
                setTab('board')
              }}
            />
          ) : rules.length ? (
            rules.map((rule) => {
              const currentState = ruleState(rule)
              return (
                <button
                  key={rule.id}
                  onClick={() => setSelectedId(rule.id)}
                  className={`mb-2 grid w-full grid-cols-[1fr_auto] gap-3 rounded border p-3 text-left ${selected?.id === rule.id ? 'border-accent bg-accent-bg' : 'border-border'}`}
                >
                  <span>
                    <b className="block text-sm">{rule.name}</b>
                    <span className="mt-1 flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
                      <span>{rule.instances?.length ?? 0} instances</span>
                      <span
                        className={`rounded px-1.5 py-0.5 ${severityTone[rule.severity] ?? 'bg-muted text-muted-foreground'}`}
                      >
                        {rule.severity || 'warning'}
                      </span>
                    </span>
                  </span>
                  <span
                    className={`h-fit rounded px-1.5 py-0.5 font-mono text-[10px] ${tone[currentState] ?? ''}`}
                  >
                    {currentState}
                  </span>
                </button>
              )
            })
          ) : (
            <Empty label="No alert rules match this view." />
          )}
          {tab === 'board' && state !== 'notifications' && (
            <PaginationControls
              page={rulePage}
              pageSize={15}
              total={ruleTotal}
              itemLabel="alert rules"
              onPageChange={setRulePage}
            />
          )}
        </div>
      </section>
      <aside className="flex-1 overflow-auto bg-surface p-5">
        {state === 'notifications' ? (
          <NotificationInspector
            openSource={(link) => {
              const id = new URLSearchParams(link.split('?')[1] ?? '').get('id')
              if (id) setLocation(id, null)
              setTab('board')
              setState('attention')
              setRulePage(1)
            }}
          />
        ) : creating ? (
          <CreateAlert
            close={() => setLocation(selectedId, null)}
            selected={(id) => {
              setSelectedId(id)
            }}
          />
        ) : importing ? (
          <ImportAlert
            close={() => setLocation(selectedId, null)}
            selected={(id) => {
              setSelectedId(id)
            }}
          />
        ) : selected ? (
          <Inspector
            rule={selected}
            showYaml={showYaml}
            editingFromURL={mode === 'edit'}
            onEditingChange={(editing) => setLocation(selectedId, editing ? 'edit' : null)}
            onDuplicate={(id) => setLocation(id, 'edit')}
          />
        ) : (
          <p className="text-sm text-muted-foreground">
            Create or load an alert rule to view its YAML.
          </p>
        )}
      </aside>
      {mode === 'yaml' && yaml !== null && (
        <Modal
          value={yaml}
          close={() => {
            setYaml(null)
            setLocation(selectedId, null)
          }}
        />
      )}
    </div>
  )
}

function notificationPageFrom(params: URLSearchParams) {
  return Math.max(1, Number(params.get('notification_page')) || 1)
}

function NotificationList() {
  const [params, setParams] = useSearchParams()
  const page = notificationPageFrom(params)
  const qc = useQueryClient()
  const notifications = useQuery({
    queryKey: [qk.notifications(), page],
    queryFn: () => api.notifications.list({ page, source: 'alert' }),
  })
  useWS((event) => {
    if (event.type === 'alert' || event.type === 'issue' || event.type === 'notification') {
      qc.invalidateQueries({ queryKey: qk.notifications() })
    }
  })
  const rows = notifications.data?.data ?? []
  const selectedID = params.get('notification') ?? rows[0]?.id
  const select = (id: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      if (id === rows[0]?.id) next.delete('notification')
      else next.set('notification', id)
      return next
    })
  return (
    <>
      <p className="mb-3 text-xs text-muted-foreground">
        Alert delivery summaries. Detailed delivery evidence stays in alert history.
      </p>
      <div className="space-y-2">
        {rows.map((notification) => (
          <button
            key={notification.id}
            onClick={() => select(notification.id)}
            className={`grid w-full grid-cols-[1fr_auto] gap-3 rounded border p-3 text-left ${
              selectedID === notification.id ? 'border-accent bg-accent-bg' : 'border-border'
            } ${notification.read_at ? 'opacity-70' : ''}`}
          >
            <span className="min-w-0">
              <b className="block truncate text-sm">{notification.title}</b>
              <span className="mt-1 flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
                <span>{notification.source}</span>
                <span
                  className={`rounded px-1.5 py-0.5 ${severityTone[notification.severity] ?? 'bg-muted'}`}
                >
                  {notification.severity}
                </span>
              </span>
            </span>
            <TimestampWithAgo nanoseconds={notification.created_at} />
          </button>
        ))}
        {!notifications.isLoading && !rows.length && <Empty label="No notifications yet." />}
      </div>
      <PaginationControls
        page={page}
        pageSize={30}
        total={notifications.data?.meta?.total ?? 0}
        itemLabel="notifications"
        onPageChange={(nextPage) =>
          setParams((current) => {
            const next = new URLSearchParams(current)
            if (nextPage === 1) next.delete('notification_page')
            else next.set('notification_page', String(nextPage))
            next.delete('notification')
            return next
          })
        }
      />
    </>
  )
}

function NotificationInspector({ openSource }: { openSource: (link: string) => void }) {
  const [params] = useSearchParams()
  const page = notificationPageFrom(params)
  const qc = useQueryClient()
  const notifications = useQuery({
    queryKey: [qk.notifications(), page],
    queryFn: () => api.notifications.list({ page, source: 'alert' }),
  })
  const rows = notifications.data?.data ?? []
  const selectedID = params.get('notification') ?? rows[0]?.id
  const notification = rows.find((item) => item.id === selectedID) ?? rows[0]
  const acknowledge = useMutation({
    mutationFn: (id: string) => api.notifications.acknowledge(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications() }),
  })
  if (!notification) {
    return <p className="text-sm text-muted-foreground">Select a notification to inspect it.</p>
  }
  return (
    <div className="space-y-5">
      <header className="border-b border-border pb-4">
        <div className="flex flex-wrap items-center gap-2">
          <p className="font-mono text-[11px] tracking-wide text-muted-foreground">NOTIFICATION</p>
          <span
            className={`rounded px-1.5 py-0.5 font-mono text-[10px] ${severityTone[notification.severity] ?? 'bg-muted'}`}
          >
            {notification.severity}
          </span>
          {notification.acknowledged_at && (
            <span className="text-xs text-ok-ink">Acknowledged</span>
          )}
        </div>
        <h2 className="mt-2 text-lg font-semibold">{notification.title}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {notification.body || 'No additional detail.'}
        </p>
      </header>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt className="text-muted-foreground">Source</dt>
        <dd className="font-mono text-xs">{notification.source}</dd>
        <dt className="text-muted-foreground">Received</dt>
        <dd>
          <TimestampWithAgo nanoseconds={notification.created_at} />
        </dd>
        <dt className="text-muted-foreground">Status</dt>
        <dd>{notification.read_at ? 'Read' : 'Unread'}</dd>
      </dl>
      <div className="flex flex-wrap gap-2">
        {!notification.acknowledged_at && (
          <button
            onClick={() => acknowledge.mutate(notification.id)}
            className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-accent-ink"
          >
            Acknowledge
          </button>
        )}
        {notification.link && (
          <button
            onClick={() => openSource(notification.link)}
            className="rounded border border-border px-3 py-1.5 text-xs"
          >
            Open source
          </button>
        )}
      </div>
    </div>
  )
}

function CreateAlert({ close, selected }: { close: () => void; selected: (id: string) => void }) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState(() => draftFor(emptyRule()))
  const save = useMutation({
    mutationFn: () => api.alerts.create(draftPayload(draft)),
    onSuccess: (result) => {
      qc.invalidateQueries({ queryKey: qk.alerts() })
      selected(result.data.id)
    },
  })
  return (
    <AlertEditor
      rule={emptyRule()}
      draft={draft}
      setDraft={setDraft}
      save={() => save.mutate()}
      cancel={close}
      saving={save.isPending}
      error={save.error?.message}
      create
    />
  )
}
function ImportAlert({ close, selected }: { close: () => void; selected: (id: string) => void }) {
  const qc = useQueryClient()
  const [yaml, setYaml] = useState(
    "version: 1\nname: New alert\nquery: |\n  SELECT count(*) AS value FROM spans\ncondition:\n  operator: '>'\n  threshold: 0\nseverity: warning\n",
  )
  const importRule = useMutation({
    mutationFn: () => api.alerts.importConfig(yaml),
    onSuccess: (response) => {
      qc.invalidateQueries({ queryKey: qk.alerts() })
      const rule = response.data as AlertRule | undefined
      if (rule?.id) selected(rule.id)
      else close()
    },
  })
  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault()
        importRule.mutate()
      }}
    >
      <header className="flex items-start justify-between border-b border-border pb-5">
        <div>
          <p className="font-mono text-[11px] uppercase tracking-wide text-muted-foreground">
            Import definition
          </p>
          <h2 className="mt-1 text-lg font-semibold">Alert YAML</h2>
        </div>
        <button
          type="button"
          onClick={close}
          className="rounded border border-border px-2.5 py-1.5 text-xs"
        >
          Cancel
        </button>
      </header>
      <p className="text-sm text-muted-foreground">
        Validation happens before the definition is saved. Existing IDs are updated; omitted IDs
        create a rule.
      </p>
      <YamlEditor value={yaml} onChange={setYaml} label="Alert YAML" />
      {importRule.error && (
        <p role="alert" className="text-sm text-danger">
          Could not import: {importRule.error.message}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={close}
          className="rounded border border-border px-3 py-2 text-sm"
        >
          Cancel
        </button>
        <button
          disabled={importRule.isPending}
          className="rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink"
        >
          {importRule.isPending ? 'Importing…' : 'Validate and import'}
        </button>
      </div>
    </form>
  )
}
function Inspector({
  rule,
  showYaml,
  editingFromURL,
  onEditingChange,
  onDuplicate,
}: {
  rule: AlertRule
  showYaml: () => void
  editingFromURL: boolean
  onEditingChange: (editing: boolean) => void
  onDuplicate: (id: string) => void
}) {
  const condition = parseJSON<{
    kind?: string
    operator?: string
    value?: number
    pattern?: string
    rule_ids?: string[]
  }>(rule.condition_json, {})
  const groupBy = parseJSON<string[]>(rule.group_by_json, [])
  const annotations = parseJSON<Record<string, string>>(rule.annotations_json, {})
  const state = ruleState(rule)
  const queryClient = useQueryClient()
  const [instancesPage, setInstancesPage] = useState(1)
  const [eventsPage, setEventsPage] = useState(1)
  const events = useQuery({
    queryKey: ['alert-events', rule.id, eventsPage],
    queryFn: () => api.alerts.events(rule.id, { page: eventsPage, limit: 15 }),
  })
  const silences = useQuery({
    queryKey: ['alert-silences', rule.id],
    queryFn: () => api.alerts.silences(rule.id).then((x) => x.data),
  })
  const acknowledge = useMutation({
    mutationFn: () => api.alerts.acknowledge(rule.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.alerts() })
      queryClient.invalidateQueries({ queryKey: ['alert-events', rule.id] })
    },
  })
  const acknowledgeInstance = useMutation({
    mutationFn: ({
      groupKey,
      acknowledged,
      note,
    }: {
      groupKey: string
      acknowledged: boolean
      note: string
    }) =>
      acknowledged
        ? api.alerts.unacknowledgeInstance(rule.id, groupKey)
        : api.alerts.acknowledgeInstance(rule.id, groupKey, note),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.alerts() })
      queryClient.invalidateQueries({ queryKey: ['alert-events', rule.id] })
    },
  })
  const remove = useMutation({
    mutationFn: () => api.alerts.remove(rule.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.alerts() })
      onEditingChange(false)
    },
  })
  const duplicate = useMutation({
    mutationFn: () => api.alerts.duplicate(rule.id),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: qk.alerts() })
      onDuplicate(result.data.id)
    },
  })
  const testNotification = useMutation({
    mutationFn: (destination: 'browser' | 'pushover') =>
      api.alerts.testNotification(rule.id, destination),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['alert-events', rule.id] })
      if (result.data.destination === 'browser') {
        void showNativeBrowserNotification(rule.name, result.data.body)
      }
    },
  })
  const [editing, setEditing] = useState(editingFromURL)
  const [ackNote, setAckNote] = useState('')
  const [silenceGroup, setSilenceGroup] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft>(() => draftFor(rule))
  const fileManaged = Boolean(rule.source_file)
  const instances = rule.instances ?? []
  const instancePageSize = 15
  const instancePageStart = (instancesPage - 1) * instancePageSize
  const visibleInstances = instances.slice(instancePageStart, instancePageStart + instancePageSize)
  const lastFiredAt = instances.reduce(
    (latest, instance) => Math.max(latest, instance.fired_at ?? 0),
    0,
  )
  useEffect(() => {
    setEditing(editingFromURL)
    setDraft(draftFor(rule))
    setInstancesPage(1)
    setEventsPage(1)
  }, [rule.id, editingFromURL])
  useEffect(() => {
    setInstancesPage((page) =>
      Math.min(page, Math.max(1, Math.ceil(instances.length / instancePageSize))),
    )
  }, [instances.length])
  useEffect(() => {
    if (!testNotification.data && !testNotification.error) return
    const timeout = window.setTimeout(() => testNotification.reset(), 12_000)
    return () => window.clearTimeout(timeout)
  }, [testNotification.data, testNotification.error, testNotification.reset])
  const save = useMutation({
    mutationFn: () => api.alerts.update(rule.id, draftPayload(draft)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.alerts() })
      queryClient.invalidateQueries({ queryKey: ['alert-history'] })
      setEditing(false)
      onEditingChange(false)
    },
  })
  if (editing) {
    return (
      <AlertEditor
        rule={rule}
        draft={draft}
        setDraft={setDraft}
        save={() => save.mutate()}
        cancel={() => {
          setDraft(draftFor(rule))
          setEditing(false)
          onEditingChange(false)
          save.reset()
        }}
        saving={save.isPending}
        error={save.error?.message}
      />
    )
  }
  return (
    <div className="space-y-6">
      <header className="border-b border-border pb-5">
        <div className="flex items-start justify-between gap-3">
          <div>
            <p className="font-mono text-[11px] uppercase tracking-wide text-muted-foreground">
              Alert rule
            </p>
            <h2 className="mt-1 text-lg font-semibold">{rule.name}</h2>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-2">
            <div className="flex items-center gap-2">
              <span
                className={`rounded px-2 py-1 font-mono text-[11px] ${tone[state] ?? 'bg-muted'}`}
              >
                {state}
              </span>
              <button
                disabled={fileManaged}
                onClick={() => {
                  setEditing(true)
                  onEditingChange(true)
                }}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                Edit
              </button>
              {rule.instances?.some(
                (instance) =>
                  ['pending', 'firing'].includes(instance.state) && !instance.acknowledged_at,
              ) && (
                <button
                  onClick={() => acknowledge.mutate()}
                  className="rounded border border-border px-2.5 py-1.5 text-xs"
                >
                  Acknowledge
                </button>
              )}
              <button
                onClick={showYaml}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                View YAML
              </button>
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={() => testNotification.mutate('browser')}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                Test browser
              </button>
              <button
                onClick={() => testNotification.mutate('pushover')}
                className="rounded border border-border px-2.5 py-1.5 text-xs"
              >
                {testNotification.isPending ? 'Sending…' : 'Test Pushover'}
              </button>
            </div>
          </div>
        </div>
        {testNotification.data && (
          <p className="mt-2 text-xs text-muted-foreground">
            Test {testNotification.data.data.destination}: {testNotification.data.data.status}
            {testNotification.data.data.body ? ` — ${testNotification.data.data.body}` : ''}
          </p>
        )}
        {testNotification.error && (
          <p className="mt-2 text-xs text-danger">{testNotification.error.message}</p>
        )}
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs">
          <div className="flex flex-wrap gap-2">
            <span className="rounded bg-muted px-2 py-1">{rule.severity}</span>
            <span className="rounded bg-muted px-2 py-1">
              {rule.enabled ? 'enabled' : 'disabled'}
            </span>
            <span className="rounded bg-muted px-2 py-1">
              {rule.instances?.length ?? 0} instances
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => duplicate.mutate()}
              className="rounded border border-border px-2.5 py-1.5 text-xs"
            >
              {duplicate.isPending ? 'Duplicating…' : 'Duplicate'}
            </button>
            <button
              disabled={fileManaged}
              onClick={() => {
                if (window.confirm(`Delete “${rule.name}”? This cannot be undone.`)) remove.mutate()
              }}
              className="rounded border border-danger px-2.5 py-1.5 text-xs text-danger"
            >
              Delete
            </button>
          </div>
        </div>
      </header>

      {fileManaged && (
        <p className="rounded border border-warn bg-warn-bg p-3 text-sm text-warn-ink">
          This rule is managed by <code>{rule.source_file}</code>. Edit that YAML file and reload
          alerts from Settings; duplicate it to start a database-managed copy.
        </p>
      )}

      <InspectorSection title="Condition">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
          <Field label="Type" value={condition.kind ?? 'threshold'} mono />
          {condition.kind === 'log_match' ? (
            <Field label="Message contains" value={condition.pattern ?? '—'} mono />
          ) : condition.kind === 'any_of' || condition.kind === 'all_of' ? (
            <Field label="Source rule IDs" value={condition.rule_ids?.join(', ') ?? '—'} mono />
          ) : (
            <>
              <Field label="Operator" value={condition.operator ?? '—'} mono />
              <Field label="Threshold" value={condition.value?.toLocaleString() ?? '—'} mono />
            </>
          )}
          <Field label="Pending for" value={formatDuration(rule.pending_for_ns)} />
          <Field label="Cooldown" value={formatDuration(rule.cooldown_ns)} />
          <Field label="Repeat every" value={formatDuration(rule.repeat_interval_ns)} />
        </dl>
      </InspectorSection>

      <InspectorSection title="Query">
        <div className="rounded border border-border bg-background p-3">
          <SqlCode value={rule.query_sql} />
        </div>
      </InspectorSection>

      <InspectorSection title="Grouping and delivery">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
          <Field
            label="Group by"
            value={groupBy.length ? groupBy.join(', ') : 'All results'}
            mono
          />
          <Field label="Browser" value={rule.browser_enabled ? 'Enabled' : 'Disabled'} />
          <Field label="Pushover" value={rule.pushover_enabled ? 'Enabled' : 'Disabled'} />
          <Field label="Owner" value={rule.owner || 'Unassigned'} />
          <Field label="Team" value={rule.team || 'Unassigned'} />
          <Field label="Query version" value={String(rule.query_version)} mono />
          {rule.instance_discovery_sql && (
            <>
              <Field
                label="Instance discovery"
                value={
                  rule.instance_discovery_interval_ns
                    ? `Every ${formatDuration(rule.instance_discovery_interval_ns)}`
                    : 'Every evaluation'
                }
              />
              <Field
                label="Target expiry"
                value={
                  rule.instance_discovery_stale_after_ns
                    ? formatDuration(rule.instance_discovery_stale_after_ns)
                    : '24h default'
                }
              />
            </>
          )}
        </dl>
        {rule.instance_discovery_sql && (
          <div className="mt-3">
            <p className="mb-1 text-xs text-muted-foreground">Discovery query</p>
            <SqlCode value={rule.instance_discovery_sql} />
          </div>
        )}
      </InspectorSection>

      <InspectorSection title="Evaluation health">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
          <Field
            label="Last evaluated"
            value={
              rule.last_evaluated_at ? (
                <TimestampWithAgo nanoseconds={rule.last_evaluated_at} />
              ) : (
                'Not yet evaluated'
              )
            }
          />
          <Field
            label="Last successful"
            value={
              rule.last_success_at ? (
                <TimestampWithAgo nanoseconds={rule.last_success_at} />
              ) : (
                'No successful evaluation'
              )
            }
          />
          <Field
            label="Last fired"
            value={lastFiredAt ? <TimestampWithAgo nanoseconds={lastFiredAt} /> : 'Never fired'}
          />
          <Field label="Query duration" value={formatDuration(rule.last_duration_ns)} mono />
          <Field
            label="Next evaluation"
            value={
              rule.next_evaluation_at ? (
                <TimestampWithAgo nanoseconds={rule.next_evaluation_at} />
              ) : (
                'Scheduled on start'
              )
            }
          />
        </dl>
        {rule.last_error && (
          <p className="mt-3 rounded border border-danger bg-danger-bg p-2 text-xs text-danger-ink">
            Last evaluator error: {rule.last_error}
          </p>
        )}
      </InspectorSection>

      {Object.keys(annotations).length > 0 && (
        <InspectorSection title="Annotations">
          <dl className="space-y-2 text-sm">
            {Object.entries(annotations).map(([key, value]) => (
              <div key={key} className="grid grid-cols-[9rem_1fr] gap-3">
                <dt className="font-mono text-xs text-muted-foreground">{key}</dt>
                <dd className="break-words">
                  {/^https?:\/\//.test(value) ? (
                    <a
                      href={value}
                      target="_blank"
                      rel="noreferrer"
                      className="text-accent underline"
                    >
                      {value}
                    </a>
                  ) : (
                    value
                  )}
                </dd>
              </div>
            ))}
          </dl>
        </InspectorSection>
      )}

      <InspectorSection title="Instances">
        {instances.length ? (
          <div className="space-y-2">
            <div className="max-h-96 space-y-2 overflow-auto pr-1">
              {visibleInstances.map((instance) => (
                <div key={instance.group_key} className="rounded border border-border p-3 text-xs">
                  <div className="flex justify-between gap-3">
                    <span
                      className={`rounded px-1.5 py-0.5 font-mono ${tone[instance.state] ?? 'bg-muted'}`}
                    >
                      {instance.acknowledged_at ? `acknowledged ${instance.state}` : instance.state}
                    </span>
                    <span>{instance.value ?? '—'}</span>
                  </div>
                  <p className="mt-2">
                    <GroupBadge groupKey={instance.group_key} />
                  </p>
                  <p className="mt-1 text-muted-foreground">
                    Evaluated <TimestampWithAgo nanoseconds={instance.last_evaluated_at} />
                    {instance.fired_at && (
                      <>
                        {' · fired '}
                        <TimestampWithAgo nanoseconds={instance.fired_at} />
                      </>
                    )}
                    {instance.resolved_at && (
                      <>
                        {' · resolved '}
                        <TimestampWithAgo nanoseconds={instance.resolved_at} />
                      </>
                    )}
                  </p>
                  {['pending', 'firing'].includes(instance.state) && (
                    <div className="mt-2">
                      {!instance.acknowledged_at && (
                        <input
                          value={ackNote}
                          onChange={(event) => setAckNote(event.target.value)}
                          placeholder="Acknowledgement note (optional)"
                          className="mb-2 w-full rounded border border-border bg-background px-2 py-1 text-xs"
                        />
                      )}
                      <div className="mt-2 flex items-center gap-2">
                        <button
                          onClick={() =>
                            acknowledgeInstance.mutate({
                              groupKey: instance.group_key,
                              acknowledged: Boolean(instance.acknowledged_at),
                              note: ackNote,
                            })
                          }
                          className={`rounded border px-2 py-1 text-xs ${
                            instance.acknowledged_at
                              ? 'border-purple-300 bg-purple-100 text-purple-800 dark:border-purple-700 dark:bg-purple-950 dark:text-purple-200'
                              : 'border-ok bg-ok-bg text-ok-ink'
                          }`}
                        >
                          {instance.acknowledged_at ? 'Unacknowledge' : 'Acknowledge'}
                        </button>
                        <button
                          onClick={() => setSilenceGroup(instance.group_key)}
                          className="rounded border border-warn bg-warn-bg px-2 py-1 text-xs text-warn-ink"
                        >
                          Silence
                        </button>
                      </div>
                      {instance.acknowledgement_note && (
                        <p className="mt-1 text-muted-foreground">
                          Acknowledged: {instance.acknowledgement_note}
                        </p>
                      )}
                    </div>
                  )}
                  {instance.last_error && <p className="mt-1 text-danger">{instance.last_error}</p>}
                </div>
              ))}
            </div>
            <PaginationControls
              page={instancesPage}
              pageSize={instancePageSize}
              total={instances.length}
              itemLabel="instances"
              onPageChange={setInstancesPage}
            />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No evaluated instances yet.</p>
        )}
      </InspectorSection>

      <InspectorSection title="Evaluation timeline">
        <EventTimeline
          events={events.data?.data ?? []}
          total={events.data?.meta?.total ?? 0}
          page={eventsPage}
          onPageChange={setEventsPage}
          threshold={condition.value}
          operator={condition.operator}
        />
      </InspectorSection>
      <InspectorSection title="Silences">
        <Silences
          ruleID={rule.id}
          rows={silences.data ?? []}
          initialGroup={silenceGroup}
          onGroupUsed={() => setSilenceGroup(null)}
        />
      </InspectorSection>

      <InspectorSection title="Rule metadata">
        <dl className="space-y-2 text-xs text-muted-foreground">
          <div className="flex justify-between gap-4">
            <dt>Created</dt>
            <dd>
              <TimestampWithAgo nanoseconds={rule.created_at} />
            </dd>
          </div>
          <div className="flex justify-between gap-4">
            <dt>Updated</dt>
            <dd>
              <TimestampWithAgo nanoseconds={rule.updated_at} />
            </dd>
          </div>
          <div className="flex justify-between gap-4">
            <dt>Rule ID</dt>
            <dd className="font-mono">{rule.id}</dd>
          </div>
          {rule.source_file && (
            <div className="flex justify-between gap-4">
              <dt>YAML source</dt>
              <dd className="max-w-[18rem] break-all font-mono">{rule.source_file}</dd>
            </div>
          )}
        </dl>
      </InspectorSection>
    </div>
  )
}
function InspectorSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </h3>
      {children}
    </section>
  )
}
function EventTimeline({
  events,
  total,
  page,
  onPageChange,
  threshold,
  operator,
}: {
  events: AlertEvent[]
  total: number
  page: number
  onPageChange: (page: number) => void
  threshold?: number
  operator?: string
}) {
  const now = useRelativeTimeNow()
  const numericEvents = events
    .slice()
    .reverse()
    .filter((event) => event.value != null)
  const values = numericEvents.map((event) => event.value as number)
  const min = Math.min(...values, threshold ?? 0),
    max = Math.max(...values, threshold ?? 1),
    spread = max - min || 1
  const chart = { width: 720, height: 260, left: 58, right: 18, top: 20, bottom: 42 }
  const plotWidth = chart.width - chart.left - chart.right
  const plotHeight = chart.height - chart.top - chart.bottom
  const yFor = (value: number) => chart.top + (1 - (value - min) / spread) * plotHeight
  const xFor = (index: number) =>
    chart.left +
    (numericEvents.length < 2 ? plotWidth / 2 : (index / (numericEvents.length - 1)) * plotWidth)
  const thresholdY = threshold == null ? null : yFor(threshold)
  const points = values.map((value, index) => `${xFor(index)},${yFor(value)}`).join(' ')
  return events.length ? (
    <div className="space-y-3">
      <div className="rounded border border-border bg-background p-4">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-x-5 gap-y-1 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">Evaluation value history</span>
          <span>
            {numericEvents.length} numeric evaluation{numericEvents.length === 1 ? '' : 's'}
          </span>
        </div>
        <svg
          viewBox={`0 0 ${chart.width} ${chart.height}`}
          className="h-64 w-full"
          role="img"
          aria-label="Alert value history"
        >
          {[0, 0.25, 0.5, 0.75, 1].map((ratio) => {
            const value = max - ratio * spread
            const y = chart.top + ratio * plotHeight
            return (
              <g key={ratio}>
                <line
                  x1={chart.left}
                  x2={chart.width - chart.right}
                  y1={y}
                  y2={y}
                  stroke="var(--line)"
                  strokeOpacity="0.9"
                />
                <text
                  x={chart.left - 10}
                  y={y + 4}
                  textAnchor="end"
                  className="fill-muted-foreground text-[11px]"
                >
                  {value.toLocaleString(undefined, { maximumFractionDigits: 2 })}
                </text>
              </g>
            )
          })}
          {thresholdY != null && (
            <g>
              <line
                x1={chart.left}
                x2={chart.width - chart.right}
                y1={thresholdY}
                y2={thresholdY}
                stroke="var(--danger-ink)"
                strokeWidth="2"
                strokeDasharray="7 5"
              />
              <text
                x={chart.width - chart.right}
                y={Math.max(chart.top + 12, thresholdY - 8)}
                textAnchor="end"
                className="fill-danger-ink text-[11px] font-medium"
              >
                threshold {(threshold ?? 0).toLocaleString()}
              </text>
            </g>
          )}
          {points && (
            <polyline
              fill="none"
              stroke="var(--accent-ink)"
              strokeWidth="4"
              strokeLinejoin="round"
              strokeLinecap="round"
              points={points}
            />
          )}
          {numericEvents.map((event, index) => (
            <circle
              key={event.id}
              cx={xFor(index)}
              cy={yFor(event.value as number)}
              r="4"
              fill="var(--accent-ink)"
            >
              <title>
                {formatTimestampWithAgo(event.created_at, now)}:{' '}
                {(event.value as number).toLocaleString()}
              </title>
            </circle>
          ))}
          {numericEvents.length > 0 && (
            <>
              <text
                x={chart.left}
                y={chart.height - 25}
                textAnchor="start"
                className="fill-muted-foreground text-[11px]"
              >
                {formatTimestampWithAgo(numericEvents[0].created_at, now)}
              </text>
              <text
                x={chart.width - chart.right}
                y={chart.height - 14}
                textAnchor="end"
                className="fill-muted-foreground text-[11px]"
              >
                {formatTimestampWithAgo(numericEvents[numericEvents.length - 1].created_at, now)}
              </text>
            </>
          )}
        </svg>
        <p className="mt-2 text-xs text-muted-foreground">
          Recorded evaluations · min {min.toLocaleString()} · max {max.toLocaleString()}
          {threshold != null ? ` · threshold ${operator ?? ''} ${threshold.toLocaleString()}` : ''}
        </p>
      </div>
      <div className="max-h-48 space-y-2 overflow-auto pr-1">
        {events.map((event) => (
          <div className="rounded border border-border p-2 text-xs" key={event.id}>
            <div className="flex justify-between gap-3">
              <span className="font-medium">{event.kind.split('_').join(' ')}</span>
              <span>
                <TimestampWithAgo nanoseconds={event.created_at} />
              </span>
            </div>
            <p className="mt-1 font-mono text-muted-foreground">
              {event.state} · <GroupBadge groupKey={event.group_key} />
              {event.value != null ? ` · value ${event.value}` : ''}
            </p>
            {event.detail && (
              <p className="mt-1 break-words text-muted-foreground">{event.detail}</p>
            )}
          </div>
        ))}
      </div>
      <PaginationControls
        page={page}
        pageSize={15}
        total={total}
        itemLabel="timeline events"
        onPageChange={onPageChange}
      />
    </div>
  ) : (
    <p className="text-sm text-muted-foreground">No evaluations recorded yet.</p>
  )
}
function Silences({
  ruleID,
  rows,
  initialGroup,
  onGroupUsed,
}: {
  ruleID: string
  rows: AlertSilence[]
  initialGroup: string | null
  onGroupUsed: () => void
}) {
  const qc = useQueryClient()
  const [duration, setDuration] = useState('1h')
  const [comment, setComment] = useState('')
  const [groupKey, setGroupKey] = useState('')
  const [startsAt, setStartsAt] = useState('')
  const [editing, setEditing] = useState<AlertSilence | null>(null)
  const [page, setPage] = useState(1)
  const pageSize = 15
  const pageStart = (page - 1) * pageSize
  const visibleRows = rows.slice(pageStart, pageStart + pageSize)
  useEffect(() => {
    if (initialGroup) setGroupKey(initialGroup)
  }, [initialGroup])
  useEffect(() => setPage(1), [ruleID])
  useEffect(() => {
    setPage((current) => Math.min(current, Math.max(1, Math.ceil(rows.length / pageSize))))
  }, [rows.length])
  const create = useMutation({
    mutationFn: () =>
      api.alerts.silence(ruleID, {
        ends_at:
          (startsAt ? new Date(startsAt).getTime() : Date.now()) * 1e6 +
          durationNS(duration, 'Silence duration'),
        comment,
        group_key: groupKey,
        starts_at: startsAt ? new Date(startsAt).getTime() * 1e6 : undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alert-silences', ruleID] })
      setComment('')
      if (initialGroup) onGroupUsed()
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.alerts.removeSilence(ruleID, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-silences', ruleID] }),
  })
  const update = useMutation({
    mutationFn: (silence: AlertSilence) =>
      api.alerts.updateSilence(ruleID, silence.id, {
        ends_at: silence.ends_at,
        starts_at: silence.starts_at,
        comment: silence.comment,
        group_key: silence.group_key,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alert-silences', ruleID] })
      setEditing(null)
    },
  })
  const now = Date.now() * 1e6
  return (
    <div className="space-y-3">
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <input
          aria-label="Silence duration"
          value={duration}
          onChange={(e) => setDuration(e.target.value)}
          className="w-20 rounded border border-border bg-background px-2 py-1 text-xs"
        />
        <input
          aria-label="Silence instance group"
          value={groupKey}
          onChange={(e) => setGroupKey(e.target.value)}
          placeholder="Instance group (optional)"
          className="min-w-40 flex-1 rounded border border-border bg-background px-2 py-1 text-xs"
        />
        <input
          aria-label="Silence start"
          type="datetime-local"
          value={startsAt}
          onChange={(e) => setStartsAt(e.target.value)}
          className="rounded border border-border bg-background px-2 py-1 text-xs"
        />
        <input
          aria-label="Silence comment"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          placeholder="Reason (optional)"
          className="min-w-40 flex-1 rounded border border-border bg-background px-2 py-1 text-xs"
        />
        <button className="rounded border border-accent-d bg-accent-bg px-2 py-1 text-xs text-accent-ink">
          {create.isPending ? 'Silencing…' : 'Silence'}
        </button>
      </form>
      {create.error && <p className="text-xs text-danger">{create.error.message}</p>}
      {rows.length ? (
        <>
          <div className="max-h-96 space-y-2 overflow-auto pr-1">
            {visibleRows.map((silence) => (
              <div key={silence.id} className="rounded border border-border p-2 text-xs">
                <div className="flex justify-between">
                  <b>{silence.ends_at > now ? 'Active' : 'Expired'}</b>
                  <span>
                    Until <TimestampWithAgo nanoseconds={silence.ends_at} />
                  </span>
                </div>
                {silence.group_key && (
                  <p className="mt-1">
                    <GroupBadge groupKey={silence.group_key} />
                  </p>
                )}
                {silence.comment && <p className="mt-1 text-muted-foreground">{silence.comment}</p>}
                {silence.ends_at > now && (
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button
                      onClick={() =>
                        update.mutate({ ...silence, ends_at: silence.ends_at + 60 * 60 * 1e9 })
                      }
                      className="rounded border border-accent-d bg-accent-bg px-2 py-1 text-xs text-accent-ink"
                    >
                      Extend 1h
                    </button>
                    <button
                      onClick={() => setEditing(silence)}
                      className="rounded border border-border px-2 py-1 text-xs"
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => revoke.mutate(silence.id)}
                      className="rounded border border-danger bg-danger-bg px-2 py-1 text-xs text-danger-ink"
                    >
                      Revoke
                    </button>
                  </div>
                )}
                {editing?.id === silence.id && (
                  <form
                    className="mt-3 grid gap-2 border-t border-border pt-3"
                    onSubmit={(event) => {
                      event.preventDefault()
                      update.mutate(editing)
                    }}
                  >
                    <input
                      aria-label="Edit silence instance group"
                      value={editing.group_key}
                      onChange={(event) =>
                        setEditing((current) =>
                          current ? { ...current, group_key: event.target.value } : current,
                        )
                      }
                      placeholder="Instance group (blank means all)"
                      className="rounded border border-border bg-background px-2 py-1 text-xs"
                    />
                    <input
                      aria-label="Edit silence start"
                      type="datetime-local"
                      value={new Date(editing.starts_at / 1e6).toISOString().slice(0, 16)}
                      onChange={(event) =>
                        setEditing((current) =>
                          current
                            ? {
                                ...current,
                                starts_at: new Date(event.target.value).getTime() * 1e6,
                              }
                            : current,
                        )
                      }
                      className="rounded border border-border bg-background px-2 py-1 text-xs"
                    />
                    <input
                      aria-label="Edit silence end"
                      type="datetime-local"
                      value={new Date(editing.ends_at / 1e6).toISOString().slice(0, 16)}
                      onChange={(event) =>
                        setEditing((current) =>
                          current
                            ? { ...current, ends_at: new Date(event.target.value).getTime() * 1e6 }
                            : current,
                        )
                      }
                      className="rounded border border-border bg-background px-2 py-1 text-xs"
                    />
                    <input
                      aria-label="Edit silence comment"
                      value={editing.comment}
                      onChange={(event) =>
                        setEditing((current) =>
                          current ? { ...current, comment: event.target.value } : current,
                        )
                      }
                      placeholder="Reason"
                      className="rounded border border-border bg-background px-2 py-1 text-xs"
                    />
                    <div className="flex gap-2">
                      <button className="rounded border border-border px-2 py-1 text-xs">
                        {update.isPending ? 'Saving…' : 'Save silence'}
                      </button>
                      <button
                        type="button"
                        onClick={() => setEditing(null)}
                        className="rounded border border-border px-2 py-1 text-xs"
                      >
                        Cancel
                      </button>
                    </div>
                  </form>
                )}
              </div>
            ))}
          </div>
          <PaginationControls
            page={page}
            pageSize={pageSize}
            total={rows.length}
            itemLabel="silences"
            onPageChange={setPage}
          />
        </>
      ) : (
        <p className="text-sm text-muted-foreground">No silences.</p>
      )}
    </div>
  )
}
function AlertEditor({
  rule,
  draft,
  setDraft,
  save,
  cancel,
  saving,
  error,
  create = false,
}: {
  rule: AlertRule
  draft: Draft
  setDraft: Dispatch<SetStateAction<Draft>>
  save: () => void
  cancel: () => void
  saving: boolean
  error?: string
  create?: boolean
}) {
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const preview = useMutation({ mutationFn: () => api.alerts.previewDraft(draftPayload(draft)) })
  return (
    <form
      className="space-y-6"
      onSubmit={(event) => {
        event.preventDefault()
        save()
      }}
    >
      <header className="flex items-start justify-between border-b border-border pb-5">
        <div>
          <p className="font-mono text-[11px] uppercase tracking-wide text-muted-foreground">
            {create ? 'Creating alert rule' : 'Editing alert rule'}
          </p>
          <h2 className="mt-1 text-lg font-semibold">{rule.name}</h2>
        </div>
        <button
          type="button"
          onClick={cancel}
          className="rounded border border-border px-2.5 py-1.5 text-xs"
        >
          Cancel
        </button>
      </header>

      <label className="block text-sm font-medium">
        Alert title
        <input
          value={draft.name}
          onChange={(event) => set('name', event.target.value)}
          className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
        />
      </label>

      <section>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Query
        </h3>
        <SqlEditor
          value={draft.query}
          onChange={(value) => set('query', value)}
          label="Alert SQL query"
        />
      </section>

      <section className="grid grid-cols-2 gap-3">
        <label className="col-span-2 text-sm font-medium">
          Condition type
          <select
            value={draft.conditionKind}
            onChange={(event) => set('conditionKind', event.target.value as Draft['conditionKind'])}
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          >
            <option value="threshold">Numeric threshold</option>
            <option value="count">Count threshold</option>
            <option value="no_data">No data / absence</option>
            <option value="log_match">Log message pattern</option>
            <option value="any_of">Composite: any rule firing</option>
            <option value="all_of">Composite: all rules firing</option>
          </select>
          {draft.conditionKind === 'no_data' && (
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              Fires when the query returns no rows. Grouping and numeric value are not used.
            </span>
          )}
          {draft.conditionKind === 'log_match' && (
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              Fires once for every returned row whose <code>message</code> contains this text.
            </span>
          )}
          {(draft.conditionKind === 'any_of' || draft.conditionKind === 'all_of') && (
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              Fires from the current firing state of its source rules. The SQL query is retained for
              provenance but is not evaluated.
            </span>
          )}
        </label>
        {(draft.conditionKind === 'threshold' ||
          draft.conditionKind === 'count' ||
          draft.conditionKind === 'log_match') && (
          <>
            <label className="text-sm font-medium">
              Operator
              <select
                value={draft.operator}
                onChange={(event) => set('operator', event.target.value)}
                className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
              >
                {['>', '>=', '<', '<=', '=', '!='].map((operator) => (
                  <option key={operator}>{operator}</option>
                ))}
              </select>
            </label>
          </>
        )}
        {(draft.conditionKind === 'threshold' ||
          draft.conditionKind === 'count' ||
          draft.conditionKind === 'log_match') && (
          <label className="text-sm font-medium">
            {draft.conditionKind === 'log_match' ? 'Matching rows' : 'Threshold'}
            <input
              inputMode="decimal"
              value={draft.threshold}
              onChange={(event) => set('threshold', event.target.value)}
              className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
            />
          </label>
        )}
        {draft.conditionKind === 'log_match' && (
          <label className="col-span-2 text-sm font-medium">
            Message contains
            <input
              value={draft.pattern}
              onChange={(event) => set('pattern', event.target.value)}
              placeholder="timeout"
              className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 font-mono text-sm"
            />
          </label>
        )}
        {(draft.conditionKind === 'any_of' || draft.conditionKind === 'all_of') && (
          <label className="col-span-2 text-sm font-medium">
            Source rule IDs
            <input
              value={draft.sourceRuleIDs}
              onChange={(event) => set('sourceRuleIDs', event.target.value)}
              placeholder="rule-id-a, rule-id-b"
              className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 font-mono text-sm"
            />
          </label>
        )}
        <label className="text-sm font-medium">
          Pending for
          <input
            value={draft.pendingFor}
            onChange={(event) => set('pendingFor', event.target.value)}
            placeholder="30s or 5m"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="text-sm font-medium">
          Cooldown
          <input
            value={draft.cooldown}
            onChange={(event) => set('cooldown', event.target.value)}
            placeholder="5m or 1h"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="text-sm font-medium">
          Repeat notification
          <input
            value={draft.repeatInterval}
            onChange={(event) => set('repeatInterval', event.target.value)}
            placeholder="15m (blank disables)"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="col-span-2 text-sm font-medium">
          Group by columns
          <input
            value={draft.groupBy}
            onChange={(event) => set('groupBy', event.target.value)}
            placeholder="service_name, region"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 font-mono text-sm"
          />
        </label>
        <div className="col-span-2 rounded border border-border bg-muted/30 p-3">
          <label className="block text-sm font-medium">
            Instance discovery query
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              Optional. Periodically discovers expected group labels, so a group remains an instance
              even while its telemetry is absent. It must return every Group by column.
            </span>
          </label>
          <div className="mt-2">
            <SqlEditor
              value={draft.discoveryQuery}
              onChange={(value) => set('discoveryQuery', value)}
              label="Instance discovery SQL query"
            />
          </div>
          {draft.discoveryQuery.trim() && (
            <div className="mt-3 grid grid-cols-2 gap-3">
              <label className="text-sm font-medium">
                Discovery interval
                <input
                  value={draft.discoveryEvery}
                  onChange={(event) => set('discoveryEvery', event.target.value)}
                  placeholder="5m (blank evaluates every alert cycle)"
                  className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm font-medium">
                Target stale after
                <input
                  value={draft.discoveryStaleAfter}
                  onChange={(event) => set('discoveryStaleAfter', event.target.value)}
                  placeholder="24h (default)"
                  className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
                />
              </label>
            </div>
          )}
        </div>
        <label className="col-span-2 text-sm font-medium">
          Severity
          <select
            value={draft.severity}
            onChange={(event) => set('severity', event.target.value)}
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          >
            {['info', 'warning', 'critical'].map((severity) => (
              <option key={severity}>{severity}</option>
            ))}
          </select>
        </label>
        <label className="text-sm font-medium">
          Owner
          <input
            value={draft.owner}
            onChange={(event) => set('owner', event.target.value)}
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="text-sm font-medium">
          Team
          <input
            value={draft.team}
            onChange={(event) => set('team', event.target.value)}
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
      </section>

      <section>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Delivery
        </h3>
        <div className="space-y-2 text-sm">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(event) => set('enabled', event.target.checked)}
            />
            Rule enabled
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.browserEnabled}
              onChange={(event) => set('browserEnabled', event.target.checked)}
            />
            Browser notifications
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.pushoverEnabled}
              onChange={(event) => set('pushoverEnabled', event.target.checked)}
            />
            Pushover notifications
          </label>
        </div>
      </section>

      <label className="block text-sm font-medium">
        Annotations (JSON)
        <textarea
          value={draft.annotations}
          onChange={(event) => set('annotations', event.target.value)}
          className="mt-1.5 min-h-28 w-full rounded border border-border bg-background p-3 font-mono text-xs"
        />
      </label>
      {error && (
        <p role="alert" className="text-sm text-danger">
          Could not save: {error}
        </p>
      )}
      {preview.error && (
        <p role="alert" className="text-sm text-danger">
          Test failed: {preview.error.message}
        </p>
      )}
      {preview.data && <PreviewResult preview={preview.data.data} />}
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={() => preview.mutate()}
          disabled={preview.isPending}
          className="rounded border border-border px-3 py-2 text-sm"
        >
          {preview.isPending ? 'Testing…' : 'Test rule'}
        </button>
        <button
          type="button"
          onClick={cancel}
          className="rounded border border-border px-3 py-2 text-sm"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          className="rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink disabled:opacity-50"
        >
          {saving ? 'Saving…' : create ? 'Create alert' : 'Save changes'}
        </button>
      </div>
    </form>
  )
}
function PreviewResult({
  preview,
}: {
  preview: {
    columns: string[]
    rows: Array<Record<string, unknown>>
    condition?: { kind?: string; operator?: string; value?: number; pattern?: string }
    notification_preview?: Array<{ destination: string; status: string; reason?: string }>
  }
}) {
  const condition = preview.condition
  const threshold = condition?.value
  const matchingRows =
    condition?.kind === 'log_match' && condition.pattern
      ? preview.rows.filter((row) => String(row.message ?? '').includes(condition.pattern ?? ''))
      : []
  const activeSources =
    condition?.kind === 'any_of' || condition?.kind === 'all_of'
      ? preview.rows.filter((row) => row.active === true)
      : []
  const breaches =
    condition?.operator && threshold != null
      ? preview.rows.filter((row) => {
          const value = Number(row.value)
          switch (condition.operator) {
            case '>':
              return value > threshold
            case '>=':
              return value >= threshold
            case '<':
              return value < threshold
            case '<=':
              return value <= threshold
            case '=':
              return value === threshold
            case '!=':
              return value !== threshold
            default:
              return false
          }
        }).length
      : 0
  return (
    <section className="rounded border border-border bg-muted/30 p-3">
      <h3 className="text-sm font-medium">Test result</h3>
      <p className="mt-1 text-xs text-muted-foreground">
        {preview.rows.length} recent rows;{' '}
        {condition?.kind === 'no_data'
          ? preview.rows.length === 0
            ? 'no rows: this rule would fire'
            : 'data is present: this rule would stay resolved'
          : condition?.kind === 'log_match'
            ? `${matchingRows.length} rows contain ${JSON.stringify(condition.pattern ?? '')}; ${condition.operator && threshold != null ? `${matchingRows.length} would breach ${condition.operator} ${threshold}` : matchingRows.length > 0 ? 'this rule would fire' : 'this rule would stay resolved'}`
            : condition?.kind === 'any_of' || condition?.kind === 'all_of'
              ? `${activeSources.length}/${preview.rows.length} source rules firing; ${condition.kind === 'all_of' ? 'all' : 'any'} ${condition.kind === 'all_of' && activeSources.length === preview.rows.length ? 'would fire' : condition.kind === 'any_of' && activeSources.length > 0 ? 'would fire' : 'would stay resolved'}`
              : condition && threshold != null
                ? `${breaches} would breach ${condition.operator} ${threshold}`
                : 'no condition verdict available'}
        . This does not save or notify.
      </p>
      {preview.notification_preview && (
        <p className="mt-1 text-xs text-muted-foreground">
          Delivery:{' '}
          {preview.notification_preview
            .map(
              (item) =>
                `${item.destination} ${item.status}${item.reason ? ` (${item.reason})` : ''}`,
            )
            .join(' · ')}
          . This test never sends a notification.
        </p>
      )}
      <div className="mt-2 max-h-44 overflow-auto">
        <table className="w-full text-left font-mono text-[11px]">
          <thead>
            <tr>
              {preview.columns.map((c) => (
                <th className="pr-3" key={c}>
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {preview.rows.slice(0, 20).map((row, i) => (
              <tr key={i}>
                {preview.columns.map((c) => (
                  <td className="pr-3" key={c}>
                    {String(row[c] ?? '')}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
function Field({
  label,
  value,
  mono = false,
}: {
  label: string
  value: ReactNode
  mono?: boolean
}) {
  return (
    <div>
      <dt className="mb-1 text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'font-mono text-xs' : ''}>{value}</dd>
    </div>
  )
}
function Empty({ label }: { label: string }) {
  return (
    <div className="rounded border border-dashed border-border p-10 text-center">
      <BellRing className="mx-auto" />
      <p className="mt-3 text-sm">{label}</p>
    </div>
  )
}
function History({
  rows,
  total,
  page,
  rules,
  filters,
  setFilters,
  setPage,
  onSelect,
}: {
  rows: Array<{
    id: string
    rule_id: string
    rule_name?: string
    group_key: string
    kind: string
    state: string
    created_at: number
  }>
  total: number
  page: number
  rules: AlertRule[]
  filters: HistoryFilters
  setFilters: (filters: HistoryFilters) => void
  setPage: (page: number) => void
  onSelect: (id: string) => void
}) {
  const set = (key: keyof HistoryFilters, value: string) => setFilters({ ...filters, [key]: value })
  const severityByRuleID = new Map(rules.map((rule) => [rule.id, rule.severity]))
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2 rounded border border-border bg-background p-2">
        <select
          aria-label="History alert rule"
          value={filters.rule_id}
          onChange={(event) => set('rule_id', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        >
          <option value="">All rules</option>
          {rules.map((rule) => (
            <option key={rule.id} value={rule.id}>
              {rule.name}
            </option>
          ))}
        </select>
        <select
          aria-label="History severity"
          value={filters.severity}
          onChange={(event) => set('severity', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        >
          <option value="">All severities</option>
          <option value="info">Info</option>
          <option value="warning">Warning</option>
          <option value="critical">Critical</option>
        </select>
        <select
          aria-label="History state"
          value={filters.state}
          onChange={(event) => set('state', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        >
          <option value="">All states</option>
          <option value="pending">Pending</option>
          <option value="firing">Firing</option>
          <option value="resolved">Resolved</option>
          <option value="error">Error</option>
        </select>
        <select
          aria-label="History event kind"
          value={filters.kind}
          onChange={(event) => set('kind', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        >
          <option value="">All activity</option>
          <option value="firing">Firing</option>
          <option value="resolved">Resolved</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="notification">Notification</option>
          <option value="evaluation_error">Evaluation error</option>
        </select>
        <input
          aria-label="History instance group"
          value={filters.group_key}
          onChange={(event) => set('group_key', event.target.value)}
          placeholder="Instance group"
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        />
        <button
          type="button"
          onClick={() => setFilters(emptyHistoryFilters())}
          className="rounded border border-border px-2 py-1 text-xs"
        >
          Clear filters
        </button>
        <input
          aria-label="History from"
          type="datetime-local"
          value={filters.from}
          onChange={(event) => set('from', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        />
        <input
          aria-label="History to"
          type="datetime-local"
          value={filters.to}
          onChange={(event) => set('to', event.target.value)}
          className="rounded border border-border bg-surface px-2 py-1 text-xs"
        />
      </div>
      {rows.length ? (
        rows.map((e) => (
          <button
            key={e.id}
            onClick={() => onSelect(e.rule_id)}
            className="mb-2 w-full rounded border border-border p-3 text-left"
          >
            <div className="flex items-center justify-between gap-2">
              <b className="text-sm">{e.rule_name || 'Deleted alert rule'}</b>
              {severityByRuleID.get(e.rule_id) && (
                <span
                  className={`rounded px-1.5 py-0.5 font-mono text-[10px] ${severityTone[severityByRuleID.get(e.rule_id) ?? ''] ?? 'bg-muted text-muted-foreground'}`}
                >
                  {severityByRuleID.get(e.rule_id)}
                </span>
              )}
            </div>
            <p className="mt-1 flex flex-wrap items-center gap-1 font-mono text-[11px]">
              <span>
                {e.kind.split('_').join(' ')} · {e.state} ·
              </span>
              <GroupBadge groupKey={e.group_key} />
              <span>·</span>
              <TimestampWithAgo nanoseconds={e.created_at} />
            </p>
          </button>
        ))
      ) : (
        <Empty label="No matching alert history." />
      )}
      <PaginationControls
        page={page}
        pageSize={50}
        total={total}
        itemLabel="history events"
        onPageChange={setPage}
      />
    </div>
  )
}
function Modal({ value, close }: { value: string; close: () => void }) {
  const parts = value.split(/(#[^\n]*|^\s*[\w_]+:|"[^"]*"|'[^']*'|\b\d+\b)/gm)
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Alert YAML configuration"
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"
    >
      <section className="w-full max-w-3xl overflow-hidden rounded-lg border border-border bg-background shadow-xl">
        <header className="flex justify-between border-b border-border px-4 py-3">
          <h2 className="font-semibold">Alert YAML configuration</h2>
          <button onClick={close} className="rounded border border-border px-2 py-1 text-xs">
            Close
          </button>
        </header>
        <pre className="max-h-[70vh] overflow-auto p-4 font-mono text-xs leading-5">
          <code>
            {parts.map((x, i) =>
              x.startsWith('#') ? (
                <span key={i} className="text-muted-foreground">
                  {x}
                </span>
              ) : /^\s*[\w_]+:$/.test(x) ? (
                <span key={i} className="font-semibold text-violet-700">
                  {x}
                </span>
              ) : /^['"]/.test(x) ? (
                <span key={i} className="text-emerald-700">
                  {x}
                </span>
              ) : /^\d+$/.test(x) ? (
                <span key={i} className="text-amber-700">
                  {x}
                </span>
              ) : (
                x
              ),
            )}
          </code>
        </pre>
      </section>
    </div>
  )
}
