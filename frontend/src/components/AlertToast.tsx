import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useWS } from '@/lib/ws'
import { showNotification } from './NotificationToast'

export default function AlertToast() {
  const qc = useQueryClient()
  useWS((event) => {
    if (event.type === 'definition_sync') {
      const { kind, status, message } = event.payload
      const label = kind === 'dashboard' ? 'Dashboards' : 'Alerts'
      const id = `definition-sync:${kind}`
      if (status === 'updating') toast.loading(`Updating ${label.toLowerCase()} from YAML…`, { id })
      else if (status === 'error') toast.error(`${label} could not update: ${message}`, { id })
      else {
        toast.success(`${label} updated from YAML`, { id })
        for (const prefix of kind === 'dashboard'
          ? ['dashboards', 'dashboard-runtime', 'dashboard-panel', 'dashboard-annotations']
          : ['alerts', 'alert-rule', 'alert-events', 'alert-history']) {
          void qc.invalidateQueries({ queryKey: [prefix] })
        }
      }
      return
    }
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
