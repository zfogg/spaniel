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
    showNotification({ title: `Alert ${p.state}`, detail, link: p.link ?? '/alerts', linkLabel: 'open alerts →', severity: p.severity === 'critical' ? 'critical' : p.severity === 'warning' ? 'warning' : 'info', dedupeKey: `alert:${p.ruleId}:${p.transition}:${Object.values(p.groupLabels ?? {}).join('|')}`, native: p.browser === true })
  })
  return null
}
