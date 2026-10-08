import { type Dispatch, type SetStateAction } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, type AlertRule } from '@/lib/api'
import { SqlEditor } from '@/components/ui/HighlightedCode'
import { Draft, alertSeverity, draftPayload } from './alert-model'
import { PreviewResult } from './PreviewResult'
import { LabelWithHelp } from './AlertFields'
export function AlertEditor({
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
          <LabelWithHelp help="Read-only DuckDB SQL. Return a numeric value column for numeric conditions; include every Group by column when grouping.">
            Query
          </LabelWithHelp>
        </h3>
        <SqlEditor
          value={draft.query}
          onChange={(value) => set('query', value)}
          label="Alert SQL query"
        />
      </section>

      <section className="grid grid-cols-2 gap-3">
        <label className="col-span-2 text-sm font-medium">
          <LabelWithHelp help="Choose how this rule interprets the query result: numeric threshold, absent data, matching log rows, or the state of other rules.">
            Condition type
          </LabelWithHelp>
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
              <LabelWithHelp help="The comparison applied to the query result and threshold.">
                Operator
              </LabelWithHelp>
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
            <LabelWithHelp
              help={
                draft.conditionKind === 'log_match'
                  ? 'How many query rows must have a matching message before the condition is true.'
                  : 'The numeric value the query result is compared against.'
              }
            >
              {draft.conditionKind === 'log_match' ? 'Matching rows' : 'Threshold'}
            </LabelWithHelp>
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
          <LabelWithHelp help="The condition must stay true continuously for this long before firing. Leave blank to fire immediately.">
            Pending for
          </LabelWithHelp>
          <input
            value={draft.pendingFor}
            onChange={(event) => set('pendingFor', event.target.value)}
            placeholder="30s or 5m"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="text-sm font-medium">
          <LabelWithHelp help="The minimum time after firing before a new firing notification can be delivered. Leave blank for no cooldown.">
            Cooldown
          </LabelWithHelp>
          <input
            value={draft.cooldown}
            onChange={(event) => set('cooldown', event.target.value)}
            placeholder="5m or 1h"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="text-sm font-medium">
          <LabelWithHelp help="How often to notify again while the same instance remains firing. Leave blank to notify only when it starts firing.">
            Repeat notification
          </LabelWithHelp>
          <input
            value={draft.repeatInterval}
            onChange={(event) => set('repeatInterval', event.target.value)}
            placeholder="15m (blank disables)"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          />
        </label>
        <label className="col-span-2 text-sm font-medium">
          <LabelWithHelp help="Comma-separated query-result columns that define independent alert instances, such as service_name and region.">
            Group by columns
          </LabelWithHelp>
          <input
            value={draft.groupBy}
            onChange={(event) => set('groupBy', event.target.value)}
            placeholder="service_name, region"
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 font-mono text-sm"
          />
        </label>
        <div className="col-span-2 rounded border border-border bg-muted/30 p-3">
          <label className="block text-sm font-medium">
            <LabelWithHelp help="Optional SQL that lists expected group labels, so Spaniel can create and retain instances even before those groups send telemetry.">
              Instance discovery query
            </LabelWithHelp>
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
                <LabelWithHelp help="How often to refresh the expected targets. Blank runs it with every alert evaluation.">
                  Discovery interval
                </LabelWithHelp>
                <input
                  value={draft.discoveryEvery}
                  onChange={(event) => set('discoveryEvery', event.target.value)}
                  placeholder="5m (blank evaluates every alert cycle)"
                  className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm font-medium">
                <LabelWithHelp help="Remove an expected target after it has been absent from discovery for this long. The default is 24 hours.">
                  Target stale after
                </LabelWithHelp>
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
          <LabelWithHelp help="Controls visual treatment and notification urgency; it does not change the rule condition.">
            Severity
          </LabelWithHelp>
          <select
            value={draft.severity}
            onChange={(event) => set('severity', alertSeverity(event.target.value))}
            className="mt-1.5 w-full rounded border border-border bg-background px-3 py-2 text-sm"
          >
            {['info', 'warning', 'critical'].map((severity) => (
              <option key={severity}>{severity}</option>
            ))}
          </select>
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
            <LabelWithHelp help="Disabled rules stay saved but are not evaluated and do not notify.">
              Rule enabled
            </LabelWithHelp>
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.browserEnabled}
              onChange={(event) => set('browserEnabled', event.target.checked)}
            />
            <LabelWithHelp help="Send this rule's deliveries to the browser/in-app notification path, subject to global notification settings.">
              Browser notifications
            </LabelWithHelp>
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.pushoverEnabled}
              onChange={(event) => set('pushoverEnabled', event.target.checked)}
            />
            <LabelWithHelp help="Send this rule's deliveries to Pushover, subject to global credentials and settings.">
              Pushover notifications
            </LabelWithHelp>
          </label>
        </div>
      </section>

      <label className="block text-sm font-medium">
        <LabelWithHelp help="Optional JSON metadata. Use it for a description, trace link, dashboard link, or runbook URL shown with the rule.">
          Annotations (JSON)
        </LabelWithHelp>
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
