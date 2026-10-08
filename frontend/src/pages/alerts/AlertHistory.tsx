import { type AlertRule } from '@/lib/api'
import PaginationControls from '@/components/PaginationControls'
import { severityTone } from './alert-styles'
import { TimestampWithAgo } from './AlertTime'
import { GroupBadge, Empty } from './AlertFields'
import { HistoryFilters, emptyHistoryFilters } from './alert-model'
export function History({
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
