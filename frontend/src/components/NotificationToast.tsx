import { Link } from 'react-router-dom'
import { toast } from 'sonner'

export type NotificationSeverity = 'info' | 'warning' | 'critical'

export type NotificationEvent = {
  title: string
  detail: string
  link: string
  linkLabel: string
  severity: NotificationSeverity
  dedupeKey: string
  native?: boolean
}

const styles: Record<NotificationSeverity, { border: string; background: string; ink: string }> = {
  info: { border: 'border-accent', background: 'bg-accent-bg', ink: 'text-accent-ink' },
  warning: { border: 'border-warn', background: 'bg-warn-bg', ink: 'text-warn-ink' },
  critical: { border: 'border-danger', background: 'bg-danger-bg', ink: 'text-danger-ink' },
}

// One presentation path for alerts and lints: severity treatment, navigation,
// deduplication, and the optional native browser delivery all live here.
export function showNotification(event: NotificationEvent) {
  const style = styles[event.severity]
  toast.custom(
    (id) => (
      <div
        className={`min-w-[260px] max-w-[340px] rounded-lg border ${style.border} ${style.background} px-3.5 py-2.5 shadow-[0_2px_12px_rgba(0,0,0,0.18)]`}
      >
        <div className={`font-mono text-[10px] font-bold uppercase tracking-[.07em] ${style.ink}`}>
          {event.title}
        </div>
        <div className="mt-1 font-mono text-[11px] text-ink2">{event.detail}</div>
        <Link
          to={event.link}
          onClick={() => toast.dismiss(id)}
          className={`mt-1.5 block font-mono text-[10px] ${style.ink} underline decoration-dotted`}
        >
          {event.linkLabel}
        </Link>
      </div>
    ),
    { id: event.dedupeKey, duration: 12_000 },
  )
  if (event.native && 'Notification' in window && Notification.permission === 'granted') {
    new Notification(`Spaniel · ${event.title}`, { body: event.detail })
  }
}
