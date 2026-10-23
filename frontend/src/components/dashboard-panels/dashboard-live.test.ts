import { expect, it } from 'vitest'
import { liveDashboardPanel } from './dashboard-live'
import type { DashboardPanel } from '@/lib/api'
it('changes the recording clock only in live mode and preserves session scoping', () => {
  const panel = {
    query_sql:
      'SELECT * FROM metrics WHERE session_id=$session_id AND timestamp_ns>=coalesce(greatest((SELECT max(timestamp_ns) FROM metrics WHERE session_id=$session_id),(SELECT max(end_ns) FROM spans WHERE session_id=$session_id)),epoch_ns(current_timestamp))-900000000000',
    settings_json: '{}',
  } as DashboardPanel
  expect(liveDashboardPanel(panel, false)).toBe(panel)
  const live = liveDashboardPanel(panel, true)
  expect(live.query_sql).toBe(
    'SELECT * FROM metrics WHERE session_id=$session_id AND timestamp_ns>=epoch_ns(current_timestamp)-900000000000',
  )
  expect(JSON.parse(live.settings_json).time_window_seconds).toBe(900)
})
