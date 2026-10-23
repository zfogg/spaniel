import type { DashboardPanel } from '@/lib/api'

export function liveDashboardPanel(panel: DashboardPanel, live: boolean): DashboardPanel {
  if (!live) return panel
  let settings: Record<string, unknown> = {}
  try {
    settings = JSON.parse(panel.settings_json)
  } catch {
    /* optional settings */
  }
  // Saved recording windows use the selected session's latest sample as their clock.
  const query = panel.query_sql.replace(
    /coalesce\(greatest\(\(SELECT max\(timestamp_ns\).*?epoch_ns\(current_timestamp\)\)/gs,
    'epoch_ns(current_timestamp)',
  )
  return {
    ...panel,
    query_sql: query,
    settings_json: JSON.stringify({ ...settings, time_window_seconds: 900 }),
  }
}
