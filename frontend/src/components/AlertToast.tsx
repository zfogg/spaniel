import { useWS } from '@/lib/ws'
import { showNotification } from './NotificationToast'

export default function AlertToast() {
  useWS((event) => {
    if (event.type !== 'alert') return
    const p = event.payload
    const group = Object.values(p.groupLabels ?? {}).join(', ')
    const state =
      p.transition === 'resolved'
        ? 'Resolved'
        : p.transition === 'acknowledged'
          ? 'Acknowledged'
          : p.severity
            ? `${p.severity[0].toUpperCase()}${p.severity.slice(1)}`
            : 'Alert'
    const detail =
      p.transition === 'resolved'
        ? `${state}: ${p.ruleName}${group ? ` — ${group}` : ''}`
        : `${state}: ${p.ruleName}${group ? ` — ${group}` : ''}${p.currentValue != null && p.operator && p.threshold != null ? ` (${p.currentValue} ${p.operator} ${p.threshold})` : ''}`
    showNotification({
      title: `Alert ${p.state}`,
      detail,
      link: p.link ?? '/alerts',
      linkLabel: 'open alerts →',
      severity:
        p.severity === 'critical' || p.severity === 'error'
          ? 'critical'
          : p.severity === 'warning'
            ? 'warning'
            : 'info',
      // Each delivered repeat is operator-relevant. Do not collapse distinct
      // websocket events into the first toast for that firing instance.
      dedupeKey: `alert:${p.ruleId}:${p.transition}:${Object.values(p.groupLabels ?? {}).join('|')}:${event.timestamp_ns}`,
      native: p.browser === true,
    })
  })
  return null
}
