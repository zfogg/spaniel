import type { DashboardPanel, QueryPreview } from '@/lib/api'

export type PanelRow = Record<string, unknown>

export interface DashboardPanelRendererProps {
  panel: DashboardPanel
  rows: PanelRow[]
  columns: QueryPreview['columns']
}
