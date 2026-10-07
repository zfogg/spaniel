import { useEffect, useRef, useState } from 'react'
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type KeyboardCoordinateGetter,
} from '@dnd-kit/core'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical, Maximize2 } from 'lucide-react'
import type { DashboardPanel } from '@/lib/api'
import { movedPanelLayout, panelLayout, type PanelLayout } from './panel-layout'

export type { PanelLayout } from './panel-layout'

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

function same(a: PanelLayout, b: PanelLayout) {
  return a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h
}

const gridKeyboardCoordinates: KeyboardCoordinateGetter = (
  event,
  { currentCoordinates, context },
) => {
  const layout = context.active?.data.current?.layout as PanelLayout | undefined
  const panelWidth = context.activeNode?.getBoundingClientRect().width ?? 0
  const column = panelWidth && layout ? panelWidth / layout.w : 100
  const row = 112
  switch (event.code) {
    case 'ArrowRight':
      return { ...currentCoordinates, x: currentCoordinates.x + column }
    case 'ArrowLeft':
      return { ...currentCoordinates, x: currentCoordinates.x - column }
    case 'ArrowDown':
      return { ...currentCoordinates, y: currentCoordinates.y + row }
    case 'ArrowUp':
      return { ...currentCoordinates, y: currentCoordinates.y - row }
    default:
      return currentCoordinates
  }
}

function CanvasPanel({
  panel,
  layout,
  panelCount,
  onEdit,
  onMove,
  onResize,
  columnWidth,
  saving,
}: {
  panel: DashboardPanel
  layout: PanelLayout
  panelCount: number
  onEdit: (panel: DashboardPanel) => void
  onMove?: (panel: DashboardPanel, direction: -1 | 1) => void
  onResize: (panel: DashboardPanel, layout: PanelLayout) => void
  columnWidth: () => number
  saving?: boolean
}) {
  const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, isDragging } =
    useDraggable({ id: panel.id, disabled: saving, data: { layout } })
  const resize = (event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    const originX = event.clientX
    const originY = event.clientY
    let latest = layout
    const update = (pointer: globalThis.PointerEvent) => {
      const dx = Math.round((pointer.clientX - originX) / columnWidth())
      const dy = Math.round((pointer.clientY - originY) / 112)
      latest = {
        ...layout,
        w: clamp(layout.w + dx, 1, 13 - layout.x),
        h: clamp(layout.h + dy, 1, 6),
      }
    }
    const finish = () => {
      window.removeEventListener('pointermove', update)
      if (!same(layout, latest)) onResize(panel, latest)
    }
    window.addEventListener('pointermove', update)
    window.addEventListener('pointerup', finish, { once: true })
  }
  return (
    <article
      ref={setNodeRef}
      style={{
        gridColumn: `${layout.x} / span ${layout.w}`,
        gridRow: `${layout.y} / span ${layout.h}`,
        transform: CSS.Translate.toString(transform),
        zIndex: isDragging ? 10 : undefined,
      }}
      className={`relative flex min-w-0 flex-col overflow-hidden rounded-md border border-border bg-surface shadow-sm ${isDragging ? 'cursor-grabbing opacity-80 ring-2 ring-accent' : ''}`}
    >
      <header className="flex shrink-0 items-center gap-1 border-b border-border bg-muted/30 px-2 py-1.5">
        <button
          ref={setActivatorNodeRef}
          type="button"
          aria-label={`Move ${panel.title}`}
          title="Drag to move · Space then arrow keys to reposition"
          {...attributes}
          {...listeners}
          className="cursor-grab touch-none rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing"
        >
          <GripVertical size={14} />
        </button>
        <button
          type="button"
          onClick={() => onEdit(panel)}
          className="min-w-0 flex-1 truncate text-left text-xs font-semibold hover:underline"
        >
          {panel.title}
        </button>
        {onMove && (
          <span className="flex">
            {panel.position > 0 && (
              <button
                type="button"
                aria-label={`Move ${panel.title} up`}
                disabled={saving}
                onClick={() => onMove(panel, -1)}
                className="px-1 text-xs disabled:opacity-30"
              >
                ↑
              </button>
            )}
            {panel.position < panelCount - 1 && (
              <button
                type="button"
                aria-label={`Move ${panel.title} down`}
                disabled={saving}
                onClick={() => onMove(panel, 1)}
                className="px-1 text-xs disabled:opacity-30"
              >
                ↓
              </button>
            )}
          </span>
        )}
        <span className="font-mono text-[9px] text-muted-foreground">
          {layout.w}×{layout.h}
        </span>
      </header>
      <p className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words px-3 py-2 pr-8 font-mono text-[10px] text-muted-foreground">
        {panel.display_type} · {panel.query_sql}
      </p>
      <button
        type="button"
        aria-label={`Resize ${panel.title}`}
        title="Drag to resize panel"
        onPointerDown={resize}
        className="absolute bottom-1 right-1 cursor-nwse-resize touch-none rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
      >
        <Maximize2 size={13} />
      </button>
    </article>
  )
}

export function DashboardCanvas({
  panels,
  onEdit,
  onCommit,
  onMove,
  saving,
}: {
  panels: DashboardPanel[]
  onEdit: (panel: DashboardPanel) => void
  onCommit: (panel: DashboardPanel, layout: PanelLayout) => void
  onMove?: (panel: DashboardPanel, direction: -1 | 1) => void
  saving?: boolean
}) {
  const canvas = useRef<HTMLDivElement>(null)
  const [layouts, setLayouts] = useState<Record<string, PanelLayout>>({})
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: gridKeyboardCoordinates }),
  )
  useEffect(
    () => setLayouts(Object.fromEntries(panels.map((panel) => [panel.id, panelLayout(panel)]))),
    [panels],
  )
  const commit = (panel: DashboardPanel, layout: PanelLayout) => {
    setLayouts((current) => ({ ...current, [panel.id]: layout }))
    onCommit(panel, layout)
  }
  const end = (event: DragEndEvent) => {
    if (saving) return
    const panel = panels.find((item) => item.id === event.active.id)
    const rect = canvas.current?.getBoundingClientRect()
    if (!panel || !rect) return
    const start = layouts[panel.id] ?? panelLayout(panel)
    const next = movedPanelLayout(start, event.delta, rect.width)
    if (!same(start, next)) commit(panel, next)
  }
  return (
    <section aria-label="Dashboard layout canvas">
      <header className="mb-3 flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">Panels</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Drag a panel by its grip. Drag its lower-right handle to resize; placement saves on
            release.
          </p>
        </div>
        <span className="font-mono text-[10px] text-muted-foreground">12-column canvas</span>
      </header>
      <DndContext sensors={sensors} onDragEnd={end}>
        <div
          ref={canvas}
          className="grid min-h-[336px] grid-cols-12 auto-rows-[112px] gap-3 rounded-lg border border-border bg-muted/30 p-3"
        >
          {panels.map((panel) => (
            <CanvasPanel
              key={panel.id}
              panel={panel}
              layout={layouts[panel.id] ?? panelLayout(panel)}
              panelCount={panels.length}
              onEdit={onEdit}
              onMove={onMove}
              onResize={commit}
              columnWidth={() => (canvas.current?.getBoundingClientRect().width ?? 1200) / 12}
              saving={saving}
            />
          ))}
          {!panels.length && (
            <div className="col-span-12 flex items-center justify-center rounded border border-dashed border-border text-sm text-muted-foreground">
              Add a panel to begin.
            </div>
          )}
        </div>
      </DndContext>
    </section>
  )
}
