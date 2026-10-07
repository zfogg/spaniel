import { useEffect, useRef, useState, type PointerEvent } from 'react'
import { GripVertical, Maximize2 } from 'lucide-react'
import type { DashboardPanel } from '@/lib/api'

export type PanelLayout = { x: number; y: number; w: number; h: number }

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

export function panelLayout(panel: DashboardPanel): PanelLayout {
  try {
    const stored = JSON.parse(panel.layout_json) as Partial<PanelLayout> & { width?: string }
    const w = stored.w ?? (stored.width === 'wide' ? 12 : 6)
    return { x: clamp(stored.x ?? 1, 1, 12), y: Math.max(1, stored.y ?? 1), w: clamp(w, 1, 12), h: clamp(stored.h ?? 1, 1, 6) }
  } catch { return { x: 1, y: 1, w: 6, h: 1 } }
}
function same(a: PanelLayout, b: PanelLayout) { return a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h }

export function DashboardCanvas({ panels, onEdit, onCommit, onMove, saving }: { panels: DashboardPanel[]; onEdit: (panel: DashboardPanel) => void; onCommit: (panel: DashboardPanel, layout: PanelLayout) => void; onMove?: (panel: DashboardPanel, direction: -1 | 1) => void; saving?: boolean }) {
  const canvas = useRef<HTMLDivElement>(null)
  const [layouts, setLayouts] = useState<Record<string, PanelLayout>>({})
  useEffect(() => setLayouts(Object.fromEntries(panels.map(panel => [panel.id, panelLayout(panel)]))), [panels])
  const begin = (panel: DashboardPanel, resize: boolean) => (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    const start = layouts[panel.id] ?? panelLayout(panel)
    const rect = canvas.current?.getBoundingClientRect()
    if (!rect || saving) return
    const column = rect.width / 12
    const row = 112
    const originX = event.clientX, originY = event.clientY
    let latest = start
    const update = (pointer: globalThis.PointerEvent) => {
      const dx = Math.round((pointer.clientX - originX) / column)
      const dy = Math.round((pointer.clientY - originY) / row)
      latest = resize
        ? { ...start, w: clamp(start.w + dx, 1, 13 - start.x), h: clamp(start.h + dy, 1, 6) }
        : { ...start, x: clamp(start.x + dx, 1, 13 - start.w), y: Math.max(1, start.y + dy) }
      setLayouts(current => same(current[panel.id] ?? start, latest) ? current : { ...current, [panel.id]: latest })
    }
    const finish = () => {
      window.removeEventListener('pointermove', update)
      window.removeEventListener('pointerup', finish)
      if (!same(start, latest)) onCommit(panel, latest)
    }
    window.addEventListener('pointermove', update)
    window.addEventListener('pointerup', finish, { once: true })
  }
  return <section aria-label="Dashboard layout canvas"><header className="mb-3 flex items-center justify-between"><div><h2 className="text-base font-semibold">Panels</h2><p className="mt-1 text-xs text-muted-foreground">Drag a panel by its grip. Drag its lower-right handle to resize; placement saves on release.</p></div><span className="font-mono text-[10px] text-muted-foreground">12-column canvas</span></header><div ref={canvas} className="grid min-h-[336px] grid-cols-12 auto-rows-[112px] gap-3 rounded-lg border border-border bg-muted/30 p-3">{panels.map(panel => {
    const layout = layouts[panel.id] ?? panelLayout(panel)
    return <article key={panel.id} style={{ gridColumn: `${layout.x} / span ${layout.w}`, gridRow: `${layout.y} / span ${layout.h}` }} className="relative min-w-0 overflow-hidden rounded-md border border-border bg-surface shadow-sm"><header className="flex items-center gap-1 border-b border-border bg-muted/30 px-2 py-1.5"><button type="button" aria-label={`Move ${panel.title}`} title="Drag to move panel" onPointerDown={begin(panel, false)} className="cursor-grab touch-none rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground active:cursor-grabbing"><GripVertical size={14}/></button><button type="button" onClick={() => onEdit(panel)} className="min-w-0 flex-1 truncate text-left text-xs font-semibold hover:underline">{panel.title}</button>{onMove && <span className="flex">{panel.position > 0 && <button type="button" aria-label={`Move ${panel.title} up`} disabled={saving} onClick={() => onMove(panel, -1)} className="px-1 text-xs disabled:opacity-30">↑</button>}{panel.position < panels.length - 1 && <button type="button" aria-label={`Move ${panel.title} down`} disabled={saving} onClick={() => onMove(panel, 1)} className="px-1 text-xs disabled:opacity-30">↓</button>}</span>}<span className="font-mono text-[9px] text-muted-foreground">{layout.w}×{layout.h}</span></header><p className="line-clamp-3 px-3 py-2 font-mono text-[10px] text-muted-foreground">{panel.display_type} · {panel.query_sql}</p><button type="button" aria-label={`Resize ${panel.title}`} title="Drag to resize panel" onPointerDown={begin(panel, true)} className="absolute bottom-1 right-1 cursor-nwse-resize touch-none rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><Maximize2 size={13}/></button></article>
  })}{!panels.length && <div className="col-span-12 flex items-center justify-center rounded border border-dashed border-border text-sm text-muted-foreground">Add a panel to begin.</div>}</div></section>
}


