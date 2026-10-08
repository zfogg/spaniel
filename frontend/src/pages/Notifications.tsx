import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { formatDistanceToNow } from 'date-fns'
import PaginationControls from '@/components/PaginationControls'
import { api } from '@/lib/api'
import { qk } from '@/lib/query'
import { useState } from 'react'
import { useWS } from '@/lib/ws'

const tone: Record<string, string> = {
  critical: 'bg-danger-bg text-danger-ink',
  error: 'bg-danger-bg text-danger-ink',
  warning: 'bg-warn-bg text-warn-ink',
  info: 'bg-accent-bg text-accent-ink',
}

export default function Notifications() {
  const [page, setPage] = useState(1)
  const qc = useQueryClient()
  const list = useQuery({
    queryKey: [qk.notifications(), page],
    queryFn: () => api.notifications.list({ page }),
  })
  useWS((event) => {
    if (event.type === 'alert' || event.type === 'issue') {
      qc.invalidateQueries({ queryKey: qk.notifications() })
    }
  })
  const mutate = useMutation({
    mutationFn: ({ id, acknowledged }: { id: string; acknowledged?: boolean }) =>
      acknowledged ? api.notifications.acknowledge(id) : api.notifications.read(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications() }),
  })
  const rows = list.data?.data ?? []
  return (
    <section className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col overflow-hidden p-6">
      <header className="mb-4">
        <h1 className="text-xl font-semibold">Notifications</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Durable alert, lint, and delivery summaries. Alert history remains the detailed audit
          trail.
        </p>
      </header>
      <div className="min-h-0 flex-1 overflow-auto rounded border border-border bg-surface">
        {rows.map((n) => (
          <article
            key={n.id}
            className={`border-b border-border p-4 ${n.read_at ? 'opacity-70' : ''}`}
          >
            <div className="flex items-start gap-3">
              <span
                className={`rounded px-2 py-0.5 font-mono text-xs ${tone[n.severity] ?? 'bg-muted'}`}
              >
                {n.severity}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <b>{n.title}</b>
                  <span className="font-mono text-xs text-muted-foreground">{n.source}</span>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">{n.body}</p>
                <p className="mt-1 font-mono text-xs text-muted-foreground">
                  {new Date(n.created_at / 1e6).toLocaleString()} ·{' '}
                  {formatDistanceToNow(n.created_at / 1e6, { addSuffix: true })}
                </p>
              </div>
              <div className="flex shrink-0 gap-2">
                {n.link && (
                  <Link className="rounded border border-border px-2 py-1 text-xs" to={n.link}>
                    Open
                  </Link>
                )}
                {!n.read_at && (
                  <button
                    className="rounded border border-border px-2 py-1 text-xs"
                    onClick={() => mutate.mutate({ id: n.id })}
                  >
                    Read
                  </button>
                )}
                {!n.acknowledged_at && (
                  <button
                    className="rounded border border-accent px-2 py-1 text-xs text-accent-ink"
                    onClick={() => mutate.mutate({ id: n.id, acknowledged: true })}
                  >
                    Acknowledge
                  </button>
                )}
              </div>
            </div>
          </article>
        ))}
        {!list.isLoading && !rows.length && (
          <p className="p-6 text-sm text-muted-foreground">No notifications yet.</p>
        )}
      </div>
      <PaginationControls
        page={page}
        pageSize={30}
        total={list.data?.meta?.total ?? 0}
        itemLabel="notifications"
        onPageChange={setPage}
      />
    </section>
  )
}
