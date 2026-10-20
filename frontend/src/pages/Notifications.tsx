import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { formatDistanceToNow } from 'date-fns'
import PaginationControls from '@/components/PaginationControls'
import { api } from '@/lib/api'
import { qk } from '@/lib/query'
import { useWS, useWSStatus } from '@/lib/ws'

const tone: Record<string, string> = {
  critical: 'bg-danger-bg text-danger-ink',
  error: 'bg-danger-bg text-danger-ink',
  warning: 'bg-warn-bg text-warn-ink',
  info: 'bg-accent-bg text-accent-ink',
}

export default function Notifications() {
  const [params, setParams] = useSearchParams()
  const rawPage = Number(params.get('page'))
  const page = Number.isSafeInteger(rawPage) && rawPage > 0 ? rawPage : 1
  const setPage = (next: number) => setParams({ page: String(next) })
  const status = useWSStatus()
  const qc = useQueryClient()
  const list = useQuery({
    queryKey: [...qk.notifications(), { page, limit: 35 }],
    queryFn: () => api.notifications.list({ page, limit: 35 }),
  })
  useWS((event) => {
    if (event.type === 'alert' || event.type === 'issue' || event.type === 'notification') {
      qc.invalidateQueries({ queryKey: qk.notifications() })
    }
  })
  const mutate = useMutation({
    mutationFn: ({ id, acknowledged }: { id: string; acknowledged?: boolean }) =>
      acknowledged ? api.notifications.acknowledge(id) : api.notifications.read(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications() }),
  })
  useEffect(() => {
    if (status.connected) qc.invalidateQueries({ queryKey: qk.notifications() })
  }, [status.connected, qc])
  const rows = list.data?.data ?? []
  return (
    <section className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col overflow-hidden p-6">
      <header className="mb-4">
        <h1 className="text-xl font-semibold">Notifications</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Past browser notifications, newest first. New notifications appear here live.
        </p>
        <p className="mt-2 text-xs text-muted-foreground" role="status">
          {status.connected ? 'Live updates connected' : 'Reconnecting to live updates…'}
        </p>
      </header>
      <div className="min-h-0 flex-1 overflow-auto rounded border border-border bg-surface">
        {list.isLoading && (
          <p className="p-6 text-sm text-muted-foreground">Loading notifications…</p>
        )}
        {list.isError && (
          <div role="alert" className="p-6 text-sm text-danger-ink">
            Could not load notifications. <button onClick={() => list.refetch()}>Retry</button>
          </div>
        )}
        {mutate.isError && <p role="alert">Could not update notification. Please try again.</p>}
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
                    disabled={mutate.isPending}
                    onClick={() => mutate.mutate({ id: n.id })}
                  >
                    Read
                  </button>
                )}
                {!n.acknowledged_at && (
                  <button
                    className="rounded border border-accent px-2 py-1 text-xs text-accent-ink"
                    disabled={mutate.isPending}
                    onClick={() => mutate.mutate({ id: n.id, acknowledged: true })}
                  >
                    Acknowledge
                  </button>
                )}
              </div>
            </div>
          </article>
        ))}
        {!list.isLoading && !list.isError && !rows.length && (
          <p className="p-6 text-sm text-muted-foreground">No notifications yet.</p>
        )}
      </div>
      <PaginationControls
        page={page}
        pageSize={35}
        total={list.data?.meta?.total ?? 0}
        itemLabel="notifications"
        onPageChange={setPage}
      />
    </section>
  )
}
