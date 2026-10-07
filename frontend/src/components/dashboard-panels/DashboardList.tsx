import { Fragment, useState } from 'react'
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Link } from 'react-router-dom'
import { GripVertical, LayoutDashboard } from 'lucide-react'
import type { Dashboard } from '@/lib/api'

function DashboardRow({
  dashboard,
  index,
  selectedId,
  href,
  pending,
  move,
}: {
  dashboard: Dashboard
  index: number
  selectedId?: string
  href: (id: string, index: number) => string
  pending: boolean
  move: (direction: -1 | 1) => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: dashboard.id,
    disabled: pending,
  })
  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0 : 1,
      }}
      className={`flex items-center border-l-2 ${selectedId === dashboard.id ? 'border-accent bg-accent-bg font-semibold text-accent-ink' : 'border-transparent text-muted-foreground hover:bg-muted'}`}
    >
      <button
        type="button"
        disabled={pending}
        aria-label={`Reorder ${dashboard.name}`}
        title="Drag to reorder · Arrow keys reorder"
        {...attributes}
        {...listeners}
        onKeyDown={(event) => {
          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
            event.preventDefault()
            move(event.key === 'ArrowUp' ? -1 : 1)
          }
        }}
        className="ml-1 cursor-grab touch-none rounded px-1 py-2 text-muted-foreground hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing disabled:cursor-wait disabled:opacity-50"
      >
        <GripVertical size={14} />
      </button>
      <Link
        aria-label={dashboard.name}
        aria-current={selectedId === dashboard.id ? 'page' : undefined}
        to={href(dashboard.id, index)}
        className="flex min-w-0 flex-1 items-center gap-2 py-2 pr-3 text-sm"
      >
        <LayoutDashboard size={14} className="shrink-0" />
        <span className="min-w-0 flex-1 truncate">{dashboard.name}</span>
        <small className="font-mono text-[10px] text-muted-foreground">
          {dashboard.panels.length}
        </small>
      </Link>
    </div>
  )
}

export function DashboardList({
  dashboards,
  selectedId,
  href,
  reorder,
  pending,
}: {
  dashboards: Dashboard[]
  selectedId?: string
  href: (id: string, index: number) => string
  reorder: (from: string, to: string) => void
  pending: boolean
}) {
  const [active, setActive] = useState<Dashboard | null>(null)
  const [overId, setOverId] = useState<string | null>(null)
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const start = (event: DragStartEvent) =>
    setActive(dashboards.find((dashboard) => dashboard.id === event.active.id) ?? null)
  const over = (event: DragOverEvent) => setOverId(event.over ? String(event.over.id) : null)
  const end = (event: DragEndEvent) => {
    setActive(null)
    setOverId(null)
    if (!pending && event.over && event.active.id !== event.over.id)
      reorder(String(event.active.id), String(event.over.id))
  }
  const activeIndex = active ? dashboards.findIndex((dashboard) => dashboard.id === active.id) : -1
  const overIndex = overId ? dashboards.findIndex((dashboard) => dashboard.id === overId) : -1
  const placeholderAfter = activeIndex < overIndex
  const placeholder = (
    <div
      aria-label="Drop dashboard here"
      className="mx-2 h-10 rounded border border-dashed border-accent bg-accent-bg/50"
    />
  )
  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={start}
      onDragOver={over}
      onDragCancel={() => {
        setActive(null)
        setOverId(null)
      }}
      onDragEnd={end}
    >
      <SortableContext
        items={dashboards.map((dashboard) => dashboard.id)}
        strategy={verticalListSortingStrategy}
      >
        <div aria-label="Dashboard list">
          {dashboards.map((dashboard, index) => {
            const showPlaceholder = active && overId === dashboard.id && active.id !== dashboard.id
            return (
              <Fragment key={dashboard.id}>
                {showPlaceholder && !placeholderAfter ? placeholder : null}
                <DashboardRow
                  dashboard={dashboard}
                  index={index}
                  selectedId={selectedId}
                  href={href}
                  pending={pending}
                  move={(direction) => {
                    const target = dashboards[index + direction]
                    if (target && !pending) reorder(dashboard.id, target.id)
                  }}
                />
                {showPlaceholder && placeholderAfter ? placeholder : null}
              </Fragment>
            )
          })}
        </div>
      </SortableContext>
      <DragOverlay dropAnimation={null}>
        {active ? (
          <div className="flex items-center gap-2 rounded border border-accent bg-surface px-3 py-2 text-sm font-semibold text-foreground shadow-lg">
            <GripVertical size={14} />
            <LayoutDashboard size={14} />
            <span>{active.name}</span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  )
}
