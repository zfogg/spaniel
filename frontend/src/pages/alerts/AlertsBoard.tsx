import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { Search } from 'lucide-react'
import { api, type AlertRule, type AlertEventsEnvelope } from '@/lib/api'
import { qk } from '@/lib/query'
import { useWS } from '@/lib/ws'
import PaginationControls from '@/components/PaginationControls'
import { AnimatePresence, motion } from 'motion/react'
import { RuleSparkline } from './AlertPresentation'
import { severityTone, tone } from './alert-styles'
import { AlertYamlModal } from './AlertYamlModal'
import { ruleListTone, ruleState, HistoryFilters } from './alert-model'
import { NotificationList, NotificationInspector } from './Notifications'
import { CreateAlert } from './CreateAlert'
import { ImportAlert } from './ImportAlert'
import { Inspector } from './AlertInspector'
import { Empty } from './AlertFields'
import { History } from './AlertHistory'
export function Alerts() {
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
  // The board needs a short recent trend for each visible rule. Fetch one
  // bounded event page and group it locally instead of issuing one /events
  // request per card.
  const boardHistory = useQuery({
    queryKey: ['alert-rule-sparklines', rules.map((rule) => rule.id)],
    queryFn: () => api.alerts.history({ page: 1, limit: 500 }),
    enabled: tab === 'board' && state !== 'notifications' && rules.length > 0,
    staleTime: 10_000,
  })
  const ruleSparklines = useMemo(() => {
    const values = new Map<string, number[]>()
    for (const event of boardHistory.data?.data ?? []) {
      if (event.kind !== 'evaluation' || event.value == null) continue
      const series = values.get(event.rule_id) ?? []
      if (series.length < 18) series.push(event.value)
      values.set(event.rule_id, series)
    }
    for (const series of values.values()) series.reverse()
    return values
  }, [boardHistory.data])
  const refreshTimer = useRef<number | null>(null)
  const refresh = () => {
    if (refreshTimer.current) return
    refreshTimer.current = window.setTimeout(() => {
      refreshTimer.current = null
      qc.invalidateQueries({ queryKey: qk.alerts() })
      qc.invalidateQueries({ queryKey: ['alert-history'] })
    }, 100)
  }
  useEffect(
    () => () => {
      if (refreshTimer.current) window.clearTimeout(refreshTimer.current)
    },
    [],
  )
  useWS((e) => {
    if (e.type === 'alert_sync' && e.payload.events && e.payload.total != null) {
      qc.setQueryData<AlertEventsEnvelope>(['alert-events', e.payload.ruleId, 1], (previous) => ({
        data: e.payload.events!,
        meta: { total: e.payload.total!, page: 1, limit: 15 },
        silences: previous?.silences ?? [],
      }))
    }
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
  const selectedRuleId = selected?.id
  useEffect(() => {
    if (mode !== 'yaml' || !selectedRuleId || yaml !== null) return
    api.alerts
      .config(selectedRuleId)
      .then(setYaml)
      .catch((error: unknown) => setYaml(error instanceof Error ? error.message : String(error)))
  }, [mode, selectedRuleId, yaml])
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
            <AnimatePresence initial={false} mode="popLayout">
              {rules.map((rule) => {
                const currentState = ruleState(rule)
                const visual = ruleListTone[currentState] ?? ruleListTone.resolved
                return (
                  <motion.button
                    key={rule.id}
                    layout="position"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.16, ease: [0.2, 0, 0, 1] }}
                    onClick={() => setSelectedId(rule.id)}
                    className="mb-2 grid w-full grid-cols-[minmax(0,1fr)_21.5rem] items-center gap-3 rounded border p-3 text-left transition-colors hover:brightness-95"
                    style={{
                      borderColor:
                        selected?.id === rule.id
                          ? 'var(--accent)'
                          : `color-mix(in oklch, ${visual.line} 36%, var(--border))`,
                      background: `color-mix(in oklch, ${visual.line} ${selected?.id === rule.id ? '15%' : '7%'}, var(--surface))`,
                      boxShadow:
                        selected?.id === rule.id
                          ? `inset 3px 0 0 ${visual.line}, inset 0 0 0 1px color-mix(in oklch, ${visual.line} 18%, transparent)`
                          : undefined,
                    }}
                  >
                    <span>
                      <b className="block text-sm" style={{ color: visual.ink }}>
                        {rule.name}
                      </b>
                      <span className="mt-1 flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
                        <span>{rule.instances?.length ?? 0} instances</span>
                        <span
                          className={`rounded px-1.5 py-0.5 ${severityTone[rule.severity] ?? 'bg-muted text-muted-foreground'}`}
                        >
                          {rule.severity || 'warning'}
                        </span>
                      </span>
                    </span>
                    <span className="flex min-w-0 items-center gap-1">
                      <RuleSparkline
                        values={ruleSparklines.get(rule.id) ?? []}
                        color={visual.line}
                      />
                      <span
                        className={`shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] ${tone[currentState] ?? ''}`}
                      >
                        {currentState}
                      </span>
                    </span>
                  </motion.button>
                )
              })}
            </AnimatePresence>
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
          <NotificationInspector />
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
          <AnimatePresence mode="wait" initial={false}>
            <motion.div
              key={selected.id}
              initial={{ opacity: 0, x: 8 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: -4 }}
              transition={{ duration: 0.16, ease: [0.2, 0, 0, 1] }}
            >
              <Inspector
                rule={selected}
                showYaml={showYaml}
                editingFromURL={mode === 'edit'}
                onEditingChange={(editing) => setLocation(selectedId, editing ? 'edit' : null)}
                onDuplicate={(id) => setLocation(id, 'edit')}
              />
            </motion.div>
          </AnimatePresence>
        ) : (
          <p className="text-sm text-muted-foreground">
            Create or load an alert rule to view its YAML.
          </p>
        )}
      </aside>
      {mode === 'yaml' && yaml !== null && (
        <AlertYamlModal
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
