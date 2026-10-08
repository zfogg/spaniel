import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type AlertSilence } from '@/lib/api'
import PaginationControls from '@/components/PaginationControls'
import { TimestampWithAgo } from './AlertTime'
import { GroupBadge } from './AlertFields'
import { durationNS } from './alert-model'
export function Silences({
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
      qc.invalidateQueries({ queryKey: ['alert-events', ruleID] })
      setComment('')
      if (initialGroup) onGroupUsed()
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.alerts.removeSilence(ruleID, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-events', ruleID] }),
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
      qc.invalidateQueries({ queryKey: ['alert-events', ruleID] })
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
