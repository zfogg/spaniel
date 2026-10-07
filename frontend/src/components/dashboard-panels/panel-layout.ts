import type { DashboardPanel } from '@/lib/api'

export type PanelLayout = { x: number; y: number; w: number; h: number }

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

export function movedPanelLayout(
  layout: PanelLayout,
  delta: { x: number; y: number },
  canvasWidth: number,
): PanelLayout {
  return {
    ...layout,
    x: clamp(layout.x + Math.round(delta.x / (canvasWidth / 12)), 1, 13 - layout.w),
    y: Math.max(1, layout.y + Math.round(delta.y / 112)),
  }
}

export function panelLayout(panel: DashboardPanel): PanelLayout {
  try {
    const stored = JSON.parse(panel.layout_json) as Partial<PanelLayout> & { width?: string }
    const w = stored.w ?? (stored.width === 'wide' ? 12 : 6)
    return {
      x: clamp(stored.x ?? 1, 1, 12),
      y: Math.max(1, stored.y ?? 1),
      w: clamp(w, 1, 12),
      h: clamp(stored.h ?? 1, 1, 6),
    }
  } catch {
    return { x: 1, y: 1, w: 6, h: 1 }
  }
}
