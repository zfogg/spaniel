import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { api } from '@/lib/api'
import { qk } from '@/lib/query'
import { useWS } from '@/lib/ws'
import PaginationControls from '@/components/PaginationControls'
import { severityTone } from './alert-styles'
import { TimestampWithAgo } from './AlertTime'
import { Empty } from './AlertFields'
function notificationPageFrom(params: URLSearchParams) {
  return Math.max(1, Number(params.get('notification_page')) || 1)
}

export function NotificationList() {
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

export function NotificationInspector() {
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
  const sourceParams = new URLSearchParams(notification.link?.split('?')[1] ?? '')
  sourceParams.set('state', 'attention')
  sourceParams.set('tab', 'board')
  sourceParams.delete('notification')
  const sourceHref = `/alerts?${sourceParams.toString()}`
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
          <a href={sourceHref} className="rounded border border-border px-3 py-1.5 text-xs">
            Open source
          </a>
        )}
      </div>
    </div>
  )
}
