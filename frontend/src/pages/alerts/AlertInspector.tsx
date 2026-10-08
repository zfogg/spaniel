import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type AlertRule } from '@/lib/api'
import { qk } from '@/lib/query'
import { SqlCode } from '@/components/ui/HighlightedCode'
import PaginationControls from '@/components/PaginationControls'
import { tone } from './alert-styles'
import {
  ruleState,
  parseJSON,
  formatDuration,
  showNativeBrowserNotification,
  Draft,
  draftFor,
  draftPayload,
} from './alert-model'
import { TimestampWithAgo } from './AlertTime'
import { GroupBadge, InspectorSection, Field, LabelWithHelp } from './AlertFields'
import { EventTimeline } from './EventTimeline'
import { Silences } from './Silences'
import { AlertEditor } from './AlertEditor'
export function Inspector({
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
  const draftContext = useRef<{ ruleId: string; editing: boolean }>()
  useEffect(() => {
    // Background rule refreshes must not replace an unsaved draft or reset pagination.
    if (draftContext.current?.ruleId === rule.id && draftContext.current.editing === editingFromURL)
      return
    draftContext.current = { ruleId: rule.id, editing: editingFromURL }
    setEditing(editingFromURL)
    setDraft(draftFor(rule))
    setInstancesPage(1)
    setEventsPage(1)
  }, [rule, editingFromURL])
  useEffect(() => {
    setInstancesPage((page) =>
      Math.min(page, Math.max(1, Math.ceil(instances.length / instancePageSize))),
    )
  }, [instances.length])
  const {
    data: notificationData,
    error: notificationError,
    reset: resetNotification,
  } = testNotification
  useEffect(() => {
    if (!notificationData && !notificationError) return
    const timeout = window.setTimeout(resetNotification, 12_000)
    return () => window.clearTimeout(timeout)
  }, [notificationData, notificationError, resetNotification])
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
          <Field
            label={
              <LabelWithHelp help="How Spaniel decides whether this rule is firing.">
                Type
              </LabelWithHelp>
            }
            value={condition.kind ?? 'threshold'}
            mono
          />
          {condition.kind === 'log_match' ? (
            <Field label="Message contains" value={condition.pattern ?? '—'} mono />
          ) : condition.kind === 'any_of' || condition.kind === 'all_of' ? (
            <Field label="Source rule IDs" value={condition.rule_ids?.join(', ') ?? '—'} mono />
          ) : (
            <>
              <Field
                label={
                  <LabelWithHelp help="Compares each numeric query result with the threshold.">
                    Operator
                  </LabelWithHelp>
                }
                value={condition.operator ?? '—'}
                mono
              />
              <Field
                label={
                  <LabelWithHelp help="The numeric value a result must cross to become true.">
                    Threshold
                  </LabelWithHelp>
                }
                value={condition.value?.toLocaleString() ?? '—'}
                mono
              />
            </>
          )}
          <Field
            label={
              <LabelWithHelp help="The condition must stay true this long before an instance fires.">
                Pending for
              </LabelWithHelp>
            }
            value={formatDuration(rule.pending_for_ns)}
          />
          <Field
            label={
              <LabelWithHelp help="After firing, suppresses a new firing notification for this long.">
                Cooldown
              </LabelWithHelp>
            }
            value={formatDuration(rule.cooldown_ns)}
          />
          <Field
            label={
              <LabelWithHelp help="While still firing, send another notification at this interval. Blank disables repeats.">
                Repeat every
              </LabelWithHelp>
            }
            value={formatDuration(rule.repeat_interval_ns)}
          />
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
            label={
              <LabelWithHelp help="Each distinct combination of these result columns becomes its own alert instance.">
                Group by
              </LabelWithHelp>
            }
            value={groupBy.length ? groupBy.join(', ') : 'All results'}
            mono
          />
          <Field
            label={
              <LabelWithHelp help="Allow this rule to create in-app and native browser notifications.">
                Browser
              </LabelWithHelp>
            }
            value={rule.browser_enabled ? 'Enabled' : 'Disabled'}
          />
          <Field
            label={
              <LabelWithHelp help="Allow this rule to send Pushover delivery, subject to global settings.">
                Pushover
              </LabelWithHelp>
            }
            value={rule.pushover_enabled ? 'Enabled' : 'Disabled'}
          />
          <Field label="Query version" value={String(rule.query_version)} mono />
          {rule.instance_discovery_sql && (
            <>
              <Field
                label={
                  <LabelWithHelp help="How often Spaniel runs the discovery query to find expected alert targets.">
                    Instance discovery
                  </LabelWithHelp>
                }
                value={
                  rule.instance_discovery_interval_ns
                    ? `Every ${formatDuration(rule.instance_discovery_interval_ns)}`
                    : 'Every evaluation'
                }
              />
              <Field
                label={
                  <LabelWithHelp help="A target is one expected group of labels from discovery. If discovery stops returning it for this long, Spaniel removes that target instead of keeping a stale instance forever.">
                    Target expiry
                  </LabelWithHelp>
                }
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
            <p className="mb-1 text-xs text-muted-foreground">
              <LabelWithHelp help="Returns expected group labels, including services that have not emitted telemetry yet. It must return every Group by column.">
                Discovery query
              </LabelWithHelp>
            </p>
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
          rows={events.data?.silences ?? []}
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
